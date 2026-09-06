import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import SettingsDialog from './SettingsDialog';
import { DialogProvider } from './DialogProvider';

const ipc = vi.hoisted(() => ({
  getProxySettings: vi.fn(),
  setProxySettings: vi.fn(),
  getTLSSettings: vi.fn(),
  setTLSSettings: vi.fn(),
  getSyncConfig: vi.fn(),
  setSyncConfig: vi.fn(),
  getVaultStatus: vi.fn(),
  unlockVault: vi.fn(),
  lockVault: vi.fn(),
  openReleasePage: vi.fn(),
  openNativeFile: vi.fn(),
  getNetworkStatus: vi.fn(),
  refreshSystemProxy: vi.fn(),
  listRemoteWorkspaces: vi.fn(),
  importRemoteWorkspace: vi.fn(),
  getRawSetting: vi.fn(() => Promise.resolve('')),
  setRawSetting: vi.fn(() => Promise.resolve(null)),
  storageStats: vi.fn(() => Promise.resolve({
    workspaces: 1, nodes: 0, examples: 0, environments: 0, history: 0, runnerRuns: 0,
    blobFiles: 0, blobBytes: 0, dbBytes: 1024, walBytes: 0,
  })),
  vacuumDb: vi.fn(() => Promise.resolve(null)),
  listBackups: vi.fn(() => Promise.resolve([])),
  runBackup: vi.fn(() => Promise.resolve('path')),
}));

vi.mock('../ipc', () => ({
  ...ipc,
  toAppError: (cause: unknown) => ({
    detail: cause instanceof Error ? cause.message : String(cause),
  }),
}));

function renderDialog() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <DialogProvider>
        <SettingsDialog onClose={vi.fn()} />
      </DialogProvider>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  ipc.getProxySettings.mockResolvedValue({ mode: 'system' });
  ipc.setProxySettings.mockResolvedValue(undefined);
  ipc.getTLSSettings.mockResolvedValue({});
  ipc.setTLSSettings.mockResolvedValue(undefined);
  ipc.getSyncConfig.mockResolvedValue({ url: '', username: '', passwordSet: false });
  ipc.setSyncConfig.mockResolvedValue(undefined);
  ipc.getVaultStatus.mockResolvedValue({ mode: 'keyring', canStore: true, fileExists: false, fileUnlocked: false });
  ipc.getNetworkStatus.mockResolvedValue({ systemProxyReachable: true });
  ipc.refreshSystemProxy.mockResolvedValue({ systemProxyReachable: true });
});

describe('SettingsDialog', () => {
  it('loads all settings on mount and shows general category by default', async () => {
    renderDialog();
    // 分类标签在渲染期 formatMessage（模块级只存 key），默认语言下可见中文标签
    expect(screen.getByRole('button', { name: '通用' })).toBeInTheDocument();
    await waitFor(() => expect(ipc.getSyncConfig).toHaveBeenCalled());
    expect(ipc.getVaultStatus).toHaveBeenCalled();
    expect(ipc.getNetworkStatus).toHaveBeenCalled();
  });

  it('switches categories and renders the sync pane with loaded config', async () => {
    ipc.getSyncConfig.mockResolvedValue({ url: 'https://dav.example.test', username: 'alice', passwordSet: true });
    renderDialog();
    await waitFor(() => expect(ipc.getSyncConfig).toHaveBeenCalled());

    fireEvent.click(screen.getByRole('button', { name: '同步' }));
    expect(screen.getByDisplayValue('https://dav.example.test')).toBeInTheDocument();
    expect(screen.getByDisplayValue('alice')).toBeInTheDocument();
  });

  it('saves all settings and shows the success message', async () => {
    renderDialog();
    await waitFor(() => expect(ipc.getVaultStatus).toHaveBeenCalled());

    fireEvent.click(screen.getByText('保存'));
    await waitFor(() => expect(ipc.setProxySettings).toHaveBeenCalled());
    expect(ipc.setTLSSettings).toHaveBeenCalled();
    expect(ipc.setSyncConfig).toHaveBeenCalled();
    expect(await screen.findByText('已保存并生效')).toBeInTheDocument();
  });

  it('surfaces save failures instead of claiming success', async () => {
    ipc.setSyncConfig.mockRejectedValue(new Error('webdav endpoint refused'));
    renderDialog();
    await waitFor(() => expect(ipc.getVaultStatus).toHaveBeenCalled());

    fireEvent.click(screen.getByText('保存'));
    expect(await screen.findByText(/webdav endpoint refused/)).toBeInTheDocument();
    expect(screen.queryByText('已保存并生效')).not.toBeInTheDocument();
  });

  it('picks a native file for TLS fields and keeps it in the form', async () => {
    ipc.openNativeFile.mockResolvedValue('C:\\certs\\ca.pem');
    renderDialog();
    await waitFor(() => expect(ipc.getVaultStatus).toHaveBeenCalled());

    fireEvent.click(screen.getByRole('button', { name: '网络' }));
    // 第一个 TLS 字段（自定义 CA 证书）的"浏览…"按钮，title 转给文件对话框
    const browse = await screen.findAllByRole('button', { name: '浏览…' });
    fireEvent.click(browse[0]);
    await waitFor(() => expect(ipc.openNativeFile).toHaveBeenCalledWith('选择 CA 证书'));
    expect(await screen.findByDisplayValue('C:\\certs\\ca.pem')).toBeInTheDocument();
  });

  it('reports native file dialog failures', async () => {
    ipc.openNativeFile.mockRejectedValue(new Error('dialog crashed'));
    renderDialog();
    await waitFor(() => expect(ipc.getVaultStatus).toHaveBeenCalled());

    fireEvent.click(screen.getByRole('button', { name: '网络' }));
    fireEvent.click((await screen.findAllByRole('button', { name: '浏览…' }))[0]);
    expect(await screen.findByText(/dialog crashed/)).toBeInTheDocument();
  });
});

  it('discovers and imports a remote workspace from the sync pane', async () => {
    ipc.getSyncConfig.mockResolvedValue({ url: 'https://dav.example.test', username: 'alice' });
    ipc.listRemoteWorkspaces.mockResolvedValue([
      { workspaceId: 'ws-r1', name: '团队集合', syncedAt: 42 },
    ]);
    ipc.importRemoteWorkspace.mockResolvedValue({ pulled: 3 });
    renderDialog();
    await waitFor(() => expect(ipc.getSyncConfig).toHaveBeenCalled());

    fireEvent.click(screen.getByRole('button', { name: '同步' }));
    fireEvent.click(screen.getByRole('button', { name: '从远端导入工作区' }));

    expect(await screen.findByText('团队集合')).toBeInTheDocument();
    expect(screen.getByText(/ws-r1/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: '导入' }));
    expect(await screen.findByText('导入成功')).toBeInTheDocument();
    expect(ipc.importRemoteWorkspace).toHaveBeenCalledWith('ws-r1');
  });

  it('shows discovery errors in the import panel', async () => {
    ipc.getSyncConfig.mockResolvedValue({ url: 'https://dav.example.test', username: 'alice' });
    ipc.listRemoteWorkspaces.mockRejectedValue(new Error('webdav unreachable'));
    renderDialog();
    await waitFor(() => expect(ipc.getSyncConfig).toHaveBeenCalled());

    fireEvent.click(screen.getByRole('button', { name: '同步' }));
    fireEvent.click(screen.getByRole('button', { name: '从远端导入工作区' }));
    expect(await screen.findByText(/webdav unreachable/)).toBeInTheDocument();
  });

describe('SettingsDialog 快捷键自定义', () => {
  it('展示默认组合并支持捕获新组合后即时保存', async () => {
    renderDialog();
    await waitFor(() => expect(ipc.getVaultStatus).toHaveBeenCalled());

    fireEvent.click(screen.getByRole('button', { name: '快捷键' }));
    const input = screen.getByTestId('hotkey-palette') as HTMLInputElement;
    await waitFor(() => expect(input).toHaveValue('Ctrl+K'));

    fireEvent.keyDown(input, { key: 'p', ctrlKey: true });
    expect(input).toHaveValue('Ctrl+P');
    await waitFor(() => expect(ipc.setRawSetting).toHaveBeenCalled());
    const call = ipc.setRawSetting.mock.calls[0] as unknown as [string, string];
    expect(call[0]).toBe('hotkeys');
    const parsed = JSON.parse(call[1]);
    expect(parsed.palette).toBe('mod+p');
    // 其余动作保持默认
    expect(parsed.send).toBe('mod+enter');
  });

  it('拒绝不安全组合并给出提示', async () => {
    renderDialog();
    await waitFor(() => expect(ipc.getVaultStatus).toHaveBeenCalled());

    fireEvent.click(screen.getByRole('button', { name: '快捷键' }));
    const input = screen.getByTestId('hotkey-palette') as HTMLInputElement;
    await waitFor(() => expect(input).toHaveValue('Ctrl+K'));

    fireEvent.keyDown(input, { key: 'g' });
    expect(screen.getAllByText(/需包含 Ctrl\/Cmd/).length).toBeGreaterThan(0);
    expect(ipc.setRawSetting).not.toHaveBeenCalled();
  });
});
