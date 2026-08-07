import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { DialogProvider } from './DialogProvider';
import RunnerDialog from './RunnerDialog';

const ipc = vi.hoisted(() => ({
  cancelRun: vi.fn(() => Promise.resolve()),
  runCollection: vi.fn(() => new Promise(() => undefined)),
}));

vi.mock('../ipc', () => ({
  cancelRun: ipc.cancelRun,
  runCollection: ipc.runCollection,
  exportReport: vi.fn(),
  openNativeFile: vi.fn(),
  readNativeTextFile: vi.fn(),
  onRunnerProgress: vi.fn(() => () => undefined),
  toAppError: (cause: unknown) => ({ detail: String(cause) }),
}));

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
});
