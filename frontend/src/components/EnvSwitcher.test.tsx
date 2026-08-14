import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { DialogProvider } from './DialogProvider';
import EnvSwitcher from './EnvSwitcher';

const ipc = vi.hoisted(() => ({
  listEnvironments: vi.fn(),
  upsertEnvironment: vi.fn(),
  deleteEnvironment: vi.fn(),
  setActiveEnvironment: vi.fn(),
}));

vi.mock('../ipc', () => ({
  ...ipc,
  toAppError: (cause: unknown) => ({
    detail: cause instanceof Error ? cause.message : String(cause),
  }),
}));

function renderSwitcher() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <DialogProvider>
        <EnvSwitcher workspaceId="workspace-1" />
      </DialogProvider>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  ipc.listEnvironments.mockResolvedValue([]);
  ipc.upsertEnvironment.mockResolvedValue({ id: 'environment-1', variables: [] });
  ipc.deleteEnvironment.mockResolvedValue(undefined);
  ipc.setActiveEnvironment.mockResolvedValue(undefined);
});

describe('EnvSwitcher failures', () => {
  it('shows load failures and retries', async () => {
    ipc.listEnvironments
      .mockRejectedValueOnce(new Error('environment database offline'))
      .mockResolvedValueOnce([]);
    renderSwitcher();

    expect(await screen.findByRole('alert')).toHaveTextContent(
      '环境加载失败：environment database offline',
    );
    fireEvent.click(screen.getByRole('button', { name: '重试' }));
    await waitFor(() => expect(ipc.listEnvironments).toHaveBeenCalledTimes(2));
  });

  it('reports environment creation failures', async () => {
    ipc.upsertEnvironment.mockRejectedValueOnce(new Error('disk full'));
    renderSwitcher();
    const manage = await screen.findByRole('button', { name: '管理' });
    await waitFor(() => expect(manage).toBeEnabled());
    fireEvent.click(manage);
    fireEvent.click(screen.getByRole('button', { name: '+ 新建环境' }));

    expect(await screen.findByRole('alertdialog')).toHaveTextContent('创建环境失败: disk full');
  });

  it('locks environment controls while activation is pending', async () => {
    ipc.listEnvironments.mockResolvedValue([
      { id: 'environment-1', name: 'Development', isActive: true, variables: [] },
      { id: 'environment-2', name: 'Staging', isActive: false, variables: [] },
    ]);
    ipc.setActiveEnvironment.mockImplementationOnce(() => new Promise(() => undefined));
    renderSwitcher();

    const switcher = await screen.findByRole('button', { name: 'Development' });
    fireEvent.click(switcher);
    fireEvent.click(screen.getByRole('button', { name: 'Staging' }));

    await waitFor(() => expect(switcher).toBeDisabled());
    expect(screen.getByRole('button', { name: '管理' })).toBeDisabled();
    expect(ipc.setActiveEnvironment).toHaveBeenCalledTimes(1);
  });
});
