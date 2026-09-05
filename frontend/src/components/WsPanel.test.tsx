import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import WsPanel from './WsPanel';

// onProtoMessage 需要捕获订阅回调以便测试内模拟后端事件
let protoSubscriber: ((m: { sessionId: string; kind: string; data: string; direction: string }) => void) | null =
  null;

const ipc = vi.hoisted(() => ({
  openSession: vi.fn(),
  sendSessionMessage: vi.fn(),
  closeSession: vi.fn(),
  onProtoMessage: vi.fn(),
}));

vi.mock('../ipc', () => ({
  ...ipc,
  toAppError: (cause: unknown) => ({
    detail: cause instanceof Error ? cause.message : String(cause),
  }),
}));

function renderPanel() {
  return render(<WsPanel onClose={vi.fn()} />);
}

beforeEach(() => {
  vi.clearAllMocks();
  protoSubscriber = null;
  ipc.openSession.mockResolvedValue(undefined);
  ipc.closeSession.mockResolvedValue(undefined);
  ipc.sendSessionMessage.mockResolvedValue(undefined);
  ipc.onProtoMessage.mockImplementation((cb: typeof protoSubscriber) => {
    protoSubscriber = cb;
    return () => {
      protoSubscriber = null;
    };
  });
});

function emitProto(m: { sessionId: string; kind: string; data: string; direction: string }) {
  act(() => {
    protoSubscriber?.(m);
  });
}

describe('WsPanel connect lifecycle', () => {
  it('connects and reflects connected state, then sends messages', async () => {
    renderPanel();
    const input = screen.getByPlaceholderText('wss://echo.websocket.org');
    fireEvent.change(input, { target: { value: 'wss://x.test/echo' } });
    fireEvent.click(screen.getByRole('button', { name: '连接' }));

    await waitFor(() => expect(ipc.openSession).toHaveBeenCalledTimes(1));
    // openSession 成功 resolve 即视为已连接（组件在 await 后置位）
    expect(screen.getByRole('button', { name: '断开' })).toBeInTheDocument();
    const sessionId = ipc.openSession.mock.calls[0][0] as string;
    expect(ipc.openSession.mock.calls[0][1]).toEqual({ protocol: 'websocket', url: 'wss://x.test/echo' });

    // 发送框启用：Ctrl+Enter 发送并清空
    const sendBox = screen.getByPlaceholderText('输入消息，Ctrl+Enter 发送');
    fireEvent.change(sendBox, { target: { value: 'hello' } });
    fireEvent.keyDown(sendBox, { key: 'Enter', ctrlKey: true });
    await waitFor(() => expect(ipc.sendSessionMessage).toHaveBeenCalledWith(sessionId, 'hello'));
    expect(sendBox).toHaveValue('');
  });

  it('guards against double connect while a connect is in flight', async () => {
    let release!: () => void;
    ipc.openSession.mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          release = resolve;
        }),
    );
    renderPanel();
    const input = screen.getByPlaceholderText('wss://echo.websocket.org');
    fireEvent.change(input, { target: { value: 'wss://x.test/echo' } });
    // 持有同一按钮元素连点两次：模拟 pending 窗口内的双击（fireEvent 会 flush 状态）
    const btn = screen.getByRole('button', { name: '连接' });
    fireEvent.click(btn);
    fireEvent.click(btn);

    expect(screen.getByRole('button', { name: '连接中…' })).toBeInTheDocument();
    // 防重入：pending 期间的第二次点击不得再次发起
    expect(ipc.openSession).toHaveBeenCalledTimes(1);
    await act(async () => {
      release();
    });
    expect(ipc.openSession).toHaveBeenCalledTimes(1);
    // 连接成功后按钮切换为断开
    expect(screen.getByRole('button', { name: '断开' })).toBeInTheDocument();
  });

  it('shows error and rolls back sessionId on connect failure', async () => {
    ipc.openSession.mockRejectedValue(new Error('dial failed: no route'));
    renderPanel();
    const input = screen.getByPlaceholderText('wss://echo.websocket.org');
    fireEvent.change(input, { target: { value: 'wss://bad.test' } });
    fireEvent.click(screen.getByRole('button', { name: '连接' }));

    expect(await screen.findByText(/dial failed/)).toBeInTheDocument();
    // 失败后按钮回到"连接"，可重试
    expect(screen.getByRole('button', { name: '连接' })).toBeInTheDocument();
  });

  it('filters inbound messages by sessionId and marks close as disconnected', async () => {
    renderPanel();
    const input = screen.getByPlaceholderText('wss://echo.websocket.org');
    fireEvent.change(input, { target: { value: 'wss://x.test/echo' } });
    fireEvent.click(screen.getByRole('button', { name: '连接' }));
    await waitFor(() => expect(ipc.openSession).toHaveBeenCalled());
    const sessionId = ipc.openSession.mock.calls[0][0] as string;

    emitProto({ sessionId, kind: 'open', data: '', direction: 'system' });
    emitProto({ sessionId, kind: 'message', data: 'hello', direction: 'in' });
    expect(screen.getByText('hello')).toBeInTheDocument();

    // 其他会话的消息不得进入时间线（sessionId 比对挡掉）
    emitProto({ sessionId: 'ws-other', kind: 'message', data: 'not mine', direction: 'in' });
    expect(screen.queryByText('not mine')).not.toBeInTheDocument();

    emitProto({ sessionId, kind: 'close', data: '', direction: 'system' });
    expect(screen.getByRole('button', { name: '连接' })).toBeInTheDocument();
  });

  it('disconnects and clears session id even when backend close fails', async () => {
    renderPanel();
    const input = screen.getByPlaceholderText('wss://echo.websocket.org');
    fireEvent.change(input, { target: { value: 'wss://x.test/echo' } });
    fireEvent.click(screen.getByRole('button', { name: '连接' }));
    const sessionId = (await waitFor(() => ipc.openSession.mock.calls[0][0])) as string;
    emitProto({ sessionId, kind: 'open', data: '', direction: 'system' });

    ipc.closeSession.mockRejectedValue(new Error('session already gone'));
    fireEvent.click(screen.getByRole('button', { name: '断开' }));

    // 关闭失败也要提示并回到未连接（状态不与实际会话脱节）
    expect(await screen.findByText(/session already gone/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '连接' })).toBeInTheDocument();
    await waitFor(() => expect(ipc.closeSession).toHaveBeenCalledWith(sessionId));
  });
});
