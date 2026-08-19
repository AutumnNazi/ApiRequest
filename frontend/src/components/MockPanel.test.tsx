// MockPanel 的初始状态查询与启停操作是两条独立的异步链。
// 若过期的 runningMocks 快照晚于新一轮操作落地，就会把 addr 覆盖成错误值：
// UI 显示"已停止"而服务实际在跑，再次点击会重启并换掉端口。
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react';
import { describe, expect, it, vi, beforeEach } from 'vitest';
import MockPanel from './MockPanel';

const ipc = vi.hoisted(() => ({
  startMockServer: vi.fn(),
  stopMockServer: vi.fn(),
  runningMocks: vi.fn(),
  onMockLog: vi.fn(() => () => {}),
  toAppError: vi.fn((e: unknown) => ({
    kind: 'unknown',
    detail: e instanceof Error ? e.message : String(e),
  })),
}));

vi.mock('../ipc', () => ipc);

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

function renderPanel(collectionId = 'c1') {
  return render(<MockPanel collectionId={collectionId} collectionName="集合1" onClose={() => {}} />);
}

beforeEach(() => {
  vi.clearAllMocks();
  ipc.onMockLog.mockReturnValue(() => {});
});

describe('MockPanel 初始查询与启停的竞态', () => {
  it('初始查询未落地前禁用启停按钮', () => {
    ipc.runningMocks.mockReturnValue(deferred<Record<string, string>>().promise);
    renderPanel();

    expect(screen.getByRole('button', { name: '启动' })).toBeDisabled();
  });

  it('初始查询落地后恢复可用', async () => {
    const initial = deferred<Record<string, string>>();
    ipc.runningMocks.mockReturnValue(initial.promise);
    renderPanel();

    await act(async () => {
      initial.resolve({});
    });

    expect(screen.getByRole('button', { name: '启动' })).toBeEnabled();
  });

  it('切换集合后忽略先前查询的晚到结果', async () => {
    const first = deferred<Record<string, string>>();
    const second = deferred<Record<string, string>>();
    ipc.runningMocks.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);

    const { rerender } = renderPanel('c1');
    rerender(<MockPanel collectionId="c2" collectionName="集合2" onClose={() => {}} />);

    await act(async () => {
      second.resolve({ c2: '127.0.0.1:9200' });
    });
    expect(screen.getByText('127.0.0.1:9200')).toBeInTheDocument();

    await act(async () => {
      first.resolve({ c1: '127.0.0.1:9100' });
    });
    expect(screen.queryByText('127.0.0.1:9100')).toBeNull();
    expect(screen.getByText('127.0.0.1:9200')).toBeInTheDocument();
  });

  it('切换集合后忽略先前启动操作的晚到结果', async () => {
    const firstQuery = deferred<Record<string, string>>();
    const secondQuery = deferred<Record<string, string>>();
    const staleStart = deferred<{ addr: string }>();
    ipc.runningMocks
      .mockReturnValueOnce(firstQuery.promise)
      .mockReturnValueOnce(secondQuery.promise);
    ipc.startMockServer.mockReturnValue(staleStart.promise);

    const { rerender } = renderPanel('c1');
    await act(async () => {
      firstQuery.resolve({});
    });
    fireEvent.click(screen.getByRole('button', { name: '启动' }));
    expect(ipc.startMockServer).toHaveBeenCalledWith('c1');

    rerender(<MockPanel collectionId="c2" collectionName="集合2" onClose={() => {}} />);

    await act(async () => {
      staleStart.resolve({ addr: '127.0.0.1:9300' });
    });
    expect(screen.queryByText('127.0.0.1:9300')).toBeNull();

    await act(async () => {
      secondQuery.resolve({});
    });
    expect(screen.getByRole('button', { name: '启动' })).toBeEnabled();
  });

  it('旧操作结束时不得提前解除新操作的 busy 状态', async () => {
    const firstQuery = deferred<Record<string, string>>();
    const secondQuery = deferred<Record<string, string>>();
    const staleStart = deferred<{ addr: string }>();
    const currentStart = deferred<{ addr: string }>();
    ipc.runningMocks
      .mockReturnValueOnce(firstQuery.promise)
      .mockReturnValueOnce(secondQuery.promise);
    ipc.startMockServer
      .mockReturnValueOnce(staleStart.promise)
      .mockReturnValueOnce(currentStart.promise);

    const { rerender } = renderPanel('c1');
    await act(async () => {
      firstQuery.resolve({});
    });
    fireEvent.click(screen.getByRole('button', { name: '启动' }));

    rerender(<MockPanel collectionId="c2" collectionName="集合2" onClose={() => {}} />);
    await act(async () => {
      secondQuery.resolve({});
    });
    const startButton = screen.getByRole('button', { name: '启动' });
    expect(startButton).toBeEnabled();
    fireEvent.click(startButton);
    expect(startButton).toBeDisabled();

    await act(async () => {
      staleStart.resolve({ addr: '127.0.0.1:9300' });
    });
    expect(screen.getByRole('button', { name: '启动' })).toBeDisabled();
    expect(screen.queryByText('127.0.0.1:9300')).toBeNull();

    await act(async () => {
      currentStart.resolve({ addr: '127.0.0.1:9400' });
    });
    expect(screen.getByText('127.0.0.1:9400')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '停止' })).toBeEnabled();
  });

  it('初始查询失败时展示错误且不留在禁用态', async () => {
    const initial = deferred<Record<string, string>>();
    ipc.runningMocks.mockReturnValue(initial.promise);

    renderPanel();
    await act(async () => {
      initial.reject(new Error('ipc down'));
    });

    expect(await screen.findByText(/ipc down/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '启动' })).toBeEnabled();
  });

  it('启动失败时展示错误并保持已停止', async () => {
    const initial = deferred<Record<string, string>>();
    ipc.runningMocks.mockReturnValue(initial.promise);
    ipc.startMockServer.mockRejectedValue(new Error('port in use'));

    renderPanel();
    await act(async () => {
      initial.resolve({});
    });

    const startButton = screen.getByRole('button', { name: '启动' });
    await waitFor(() => expect(startButton).toBeEnabled());
    fireEvent.click(startButton);

    expect(await screen.findByText(/port in use/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '启动' })).toBeEnabled();
  });
});
