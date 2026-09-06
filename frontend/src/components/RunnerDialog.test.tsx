import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { DialogProvider } from './DialogProvider';
import RunnerDialog from './RunnerDialog';

const ipc = vi.hoisted(() => ({
  cancelRun: vi.fn(() => Promise.resolve()),
  runCollection: vi.fn(() => new Promise(() => undefined)),
  exportReport: vi.fn(),
  exportReportHTML: vi.fn(),
  saveNativeFile: vi.fn(),
  writeNativeTextFile: vi.fn(() => Promise.resolve()),
  listRunnerRuns: vi.fn(),
  getRunnerRun: vi.fn(),
  deleteRunnerRun: vi.fn(() => Promise.resolve()),
  clearRunnerRuns: vi.fn(() => Promise.resolve()),
}));

vi.mock('../ipc', () => ({
  cancelRun: ipc.cancelRun,
  runCollection: ipc.runCollection,
  exportReport: ipc.exportReport,
  exportReportHTML: ipc.exportReportHTML,
  saveNativeFile: ipc.saveNativeFile,
  writeNativeTextFile: ipc.writeNativeTextFile,
  listRunnerRuns: ipc.listRunnerRuns,
  getRunnerRun: ipc.getRunnerRun,
  deleteRunnerRun: ipc.deleteRunnerRun,
  clearRunnerRuns: ipc.clearRunnerRuns,
  openNativeFile: vi.fn(),
  readNativeTextFile: vi.fn(),
  onRunnerProgress: vi.fn(() => () => undefined),
  toAppError: (cause: unknown) => ({
    detail: cause instanceof Error ? cause.message : String(cause),
  }),
}));

beforeEach(() => {
  vi.clearAllMocks();
  ipc.cancelRun.mockResolvedValue(undefined);
  ipc.runCollection.mockImplementation(() => new Promise(() => undefined));
});

describe('RunnerDialog lifecycle', () => {
  it('confirms and cancels an active run before closing', async () => {
    const onClose = vi.fn();
    render(
      <DialogProvider>
        <RunnerDialog
          workspaceId="workspace-1"
          collectionId="collection-1"
          collectionName="slow collection"
          onClose={onClose}
        />
      </DialogProvider>,
    );

    fireEvent.click(screen.getByRole('button', { name: '开始运行' }));
    await waitFor(() => expect(ipc.runCollection).toHaveBeenCalledTimes(1));
    fireEvent.click(screen.getByRole('button', { name: '关闭 Runner' }));
    expect(screen.getByText('Runner 正在运行，是否取消并关闭？')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: '取消并关闭' }));

    await waitFor(() => {
      expect(ipc.cancelRun).toHaveBeenCalledTimes(1);
      expect(onClose).toHaveBeenCalledTimes(1);
    });
  });

  it('shows export failures instead of leaving an unhandled rejection', async () => {
    ipc.runCollection.mockResolvedValueOnce({
      runId: 'run-export',
      passed: 1,
      failed: 0,
      skipped: 0,
      durationMs: 10,
      canceled: false,
      results: [],
    });
    ipc.exportReport.mockRejectedValueOnce(new Error('clipboard unavailable'));
    render(
      <DialogProvider>
        <RunnerDialog
          workspaceId="workspace-1"
          collectionId="collection-1"
          collectionName="export collection"
          onClose={vi.fn()}
        />
      </DialogProvider>,
    );

    fireEvent.click(screen.getByRole('button', { name: '开始运行' }));
    fireEvent.click(await screen.findByRole('button', { name: '复制 JSON' }));

    expect(await screen.findByText('clipboard unavailable')).toBeInTheDocument();
  });

  it('exports a self-contained HTML report to the chosen path', async () => {
    ipc.runCollection.mockResolvedValueOnce({
      runId: 'run-html',
      passed: 1,
      failed: 0,
      skipped: 0,
      durationMs: 10,
      canceled: false,
      results: [],
    });
    ipc.saveNativeFile.mockResolvedValueOnce('C:/reports/run.html');
    ipc.exportReportHTML.mockResolvedValueOnce('<!DOCTYPE html><html>report</html>');
    render(
      <DialogProvider>
        <RunnerDialog
          workspaceId="workspace-1"
          collectionId="collection-1"
          collectionName="html collection"
          onClose={vi.fn()}
        />
      </DialogProvider>,
    );

    fireEvent.click(screen.getByRole('button', { name: '开始运行' }));
    fireEvent.click(await screen.findByRole('button', { name: '导出 HTML' }));

    await waitFor(() => expect(ipc.exportReportHTML).toHaveBeenCalledWith('run-html'));
    expect(ipc.writeNativeTextFile).toHaveBeenCalledWith(
      'C:/reports/run.html',
      '<!DOCTYPE html><html>report</html>',
    );
    expect(await screen.findByText('HTML 报告已保存')).toBeInTheDocument();
  });

  it('does nothing when the HTML save dialog is dismissed', async () => {
    ipc.runCollection.mockResolvedValueOnce({
      runId: 'run-html-cancel',
      passed: 1, failed: 0, skipped: 0, durationMs: 5, canceled: false, results: [],
    });
    ipc.saveNativeFile.mockResolvedValueOnce('');
    render(
      <DialogProvider>
        <RunnerDialog
          workspaceId="workspace-1"
          collectionId="collection-1"
          collectionName="html cancel collection"
          onClose={vi.fn()}
        />
      </DialogProvider>,
    );

    fireEvent.click(screen.getByRole('button', { name: '开始运行' }));
    fireEvent.click(await screen.findByRole('button', { name: '导出 HTML' }));

    await waitFor(() => expect(ipc.saveNativeFile).toHaveBeenCalled());
    expect(ipc.exportReportHTML).not.toHaveBeenCalled();
    expect(ipc.writeNativeTextFile).not.toHaveBeenCalled();
  });
});

// ── 运行历史（持久化报告回看）──
describe('RunnerDialog run history', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    ipc.cancelRun.mockResolvedValue(undefined);
    ipc.runCollection.mockImplementation(() => new Promise(() => undefined));
    ipc.listRunnerRuns.mockResolvedValue({
      items: [
        { runId: 'run-2', collectionId: 'collection-1', total: 3, passed: 2, failed: 1, skipped: 0,
          durationMs: 1500, canceled: false, createdAt: 1730000000000 },
        { runId: 'run-1', collectionId: 'collection-1', total: 2, passed: 2, failed: 0, skipped: 0,
          durationMs: 800, canceled: true, createdAt: 1720000000000 },
      ],
      nextCursor: '', hasMore: false,
    });
  });

  function renderDialog(onClose = vi.fn()) {
    return render(
      <DialogProvider>
        <RunnerDialog
          workspaceId="workspace-1"
          collectionId="collection-1"
          collectionName="history collection"
          onClose={onClose}
        />
      </DialogProvider>,
    );
  }

  it('loads persisted runs into the history view', async () => {
    renderDialog();
    fireEvent.click(screen.getByRole('button', { name: '运行历史' }));
    await screen.findByText('run-2');
    expect(ipc.listRunnerRuns).toHaveBeenCalledWith('workspace-1', { collectionId: 'collection-1' });
    expect(screen.getByText('已取消')).toBeInTheDocument();
  });

  it('reloads the list after deleting a run', async () => {
    renderDialog();
    fireEvent.click(screen.getByRole('button', { name: '运行历史' }));
    await screen.findByText('run-2');
    fireEvent.click(screen.getAllByTitle('删除该次运行')[0]);
    await waitFor(() => expect(ipc.deleteRunnerRun).toHaveBeenCalledWith('workspace-1', 'run-2'));
    expect(await screen.findByText('run-1')).toBeInTheDocument();
    expect(screen.queryByText('run-2')).not.toBeInTheDocument();
  });

  it('shows the historical report in the report view when picked', async () => {
    ipc.getRunnerRun.mockResolvedValue({
      runId: 'run-2', total: 3, passed: 2, failed: 1, skipped: 0, durationMs: 1500,
      canceled: false, createdAt: 1730000000000,
      results: [
        { iteration: 1, requestName: 'ok request', nodeId: 'n1', status: 200, durationMs: 10, failed: false },
      ],
    });
    renderDialog();
    fireEvent.click(screen.getByRole('button', { name: '运行历史' }));
    fireEvent.click(await screen.findByText('run-2'));
    expect(await screen.findByText('ok request')).toBeInTheDocument();
    // 历史列表退出（header 切换按钮保留，可再进入），报告视图出现
    expect(screen.queryByText('run-1')).not.toBeInTheDocument();
  });

  it('surfaces list failures inline', async () => {
    ipc.listRunnerRuns.mockRejectedValueOnce(new Error('db locked'));
    renderDialog();
    fireEvent.click(screen.getByRole('button', { name: '运行历史' }));
    expect(await screen.findByText('db locked')).toBeInTheDocument();
  });
});

// ── 断言聚合视图 ──
describe('RunnerDialog assertion summary', () => {
  it('renders the collapsed aggregation section when the report has assertions', async () => {
    ipc.runCollection.mockResolvedValueOnce({
      runId: 'run-agg',
      total: 2, passed: 1, failed: 1, skipped: 0, durationMs: 20, canceled: false,
      results: [
        { iteration: 1, requestName: 'login', nodeId: 'n1', status: 200, durationMs: 10, failed: false,
          testResults: [{ name: 'status is 2xx', pass: true }] },
        { iteration: 2, requestName: 'login', nodeId: 'n1', status: 500, durationMs: 10, failed: true,
          testResults: [{ name: 'status is 2xx', pass: false, error: '500 != 2xx' }] },
      ],
    } as never);
    render(
      <DialogProvider>
        <RunnerDialog
          workspaceId="workspace-1"
          collectionId="collection-1"
          collectionName="agg collection"
          onClose={vi.fn()}
        />
      </DialogProvider>,
    );
    fireEvent.click(screen.getByRole('button', { name: '开始运行' }));
    const summary = await screen.findByText(/断言聚合/);
    expect(summary).toBeInTheDocument();
    fireEvent.click(summary);
    expect(await screen.findByText('失败于：login')).toBeInTheDocument();
    expect(screen.getByText(/1\/2/)).toBeInTheDocument();
  });

  it('omits the section when no assertions ran', async () => {
    ipc.runCollection.mockResolvedValueOnce({
      runId: 'run-plain',
      total: 1, passed: 1, failed: 0, skipped: 0, durationMs: 5, canceled: false,
      results: [
        { iteration: 1, requestName: 'plain', nodeId: 'n1', status: 200, durationMs: 5, failed: false },
      ],
    } as never);
    render(
      <DialogProvider>
        <RunnerDialog
          workspaceId="workspace-1"
          collectionId="collection-1"
          collectionName="plain collection"
          onClose={vi.fn()}
        />
      </DialogProvider>,
    );
    fireEvent.click(screen.getByRole('button', { name: '开始运行' }));
    await screen.findByText('plain');
    expect(screen.queryByText(/断言聚合/)).not.toBeInTheDocument();
  });
});
