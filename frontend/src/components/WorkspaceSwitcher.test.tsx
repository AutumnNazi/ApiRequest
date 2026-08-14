import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { DialogProvider } from './DialogProvider';
import WorkspaceSwitcher from './WorkspaceSwitcher';

const ipc = vi.hoisted(() => ({
  listWorkspaces: vi.fn(),
  createWorkspace: vi.fn(),
  renameWorkspace: vi.fn(),
  deleteWorkspace: vi.fn(),
}));

vi.mock('../ipc', () => ({
  ...ipc,
  toAppError: (cause: unknown) => ({
    detail: cause instanceof Error ? cause.message : String(cause),
  }),
}));

beforeEach(() => {
  vi.clearAllMocks();
  ipc.listWorkspaces.mockResolvedValue([]);
});

describe('WorkspaceSwitcher failures', () => {
  it('shows list failures and retries', async () => {
    ipc.listWorkspaces
      .mockRejectedValueOnce(new Error('workspace database offline'))
      .mockResolvedValueOnce([]);
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={client}>
        <DialogProvider>
          <WorkspaceSwitcher activeId="workspace-1" onSwitch={vi.fn()} />
        </DialogProvider>
      </QueryClientProvider>,
    );

    expect(await screen.findByRole('alert')).toHaveTextContent(
      '工作区加载失败：workspace database offline',
    );
    fireEvent.click(screen.getByRole('button', { name: '重试' }));
    await waitFor(() => expect(ipc.listWorkspaces).toHaveBeenCalledTimes(2));
  });
});
