import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import App from './App';
import { DialogProvider } from './components/DialogProvider';
import { useTabs } from './stores/tabs';

// App 级冒烟：真实渲染 App + CookieManager（对话框编排的真实缝隙），
// 重依赖子组件用轻桩替换，ipc 全量 mock（App 自身挂载路径 + CookieManager）。
const ipc = vi.hoisted(() => ({
  getDefaultWorkspace: vi.fn(() =>
    Promise.resolve({ id: 'ws-1', name: '测试工作区', type: 'local', createdAt: 0, updatedAt: 0 }),
  ),
  renameWorkspace: vi.fn(() => Promise.resolve()),
  sendRequest: vi.fn(() => new Promise(() => undefined)),
  cancelRequest: vi.fn(() => Promise.resolve()),
  releaseResponseBlob: vi.fn(() => Promise.resolve()),
  upsertNode: vi.fn(() => Promise.resolve({})),
  getNode: vi.fn(() => Promise.reject(new Error('not found'))),
  onAutoSync: vi.fn(() => () => undefined),
  onRequestProgress: vi.fn(() => () => undefined),
  onApplicationCloseRequest: vi.fn(() => () => undefined),
  getSyncConfig: vi.fn(() => Promise.resolve({ url: '', username: '' })),
  syncNow: vi.fn(() => Promise.resolve({ ok: true })),
  getRawSetting: vi.fn(() => Promise.resolve('')),
  requestApplicationQuit: vi.fn(() => Promise.resolve()),
  newDefaultRequest: vi.fn(() => ({
    method: 'GET', url: '', headers: [], queries: [], body: { kind: 'none', content: '' },
    settings: { followRedirects: true, verifyTLS: true, timeoutMs: 30000 },
  })),
  // CookieManager 的依赖
  listCookies: vi.fn(() => Promise.resolve([])),
  clearCookies: vi.fn(() => Promise.resolve()),
  deleteCookie: vi.fn(() => Promise.resolve()),
  upsertCookie: vi.fn(() => Promise.resolve()),
  upsertCookies: vi.fn(() => Promise.resolve()),
  openNativeFile: vi.fn(() => Promise.resolve('')),
  readNativeTextFile: vi.fn(() => Promise.resolve('')),
}));

vi.mock('./ipc', () => ({
  ...ipc,
  toAppError: (cause: unknown) => ({
    detail: cause instanceof Error ? cause.message : String(cause),
  }),
}));

vi.mock('../wailsjs/runtime/runtime', () => ({
  WindowMaximise: vi.fn(),
  WindowUnmaximise: vi.fn(),
  WindowIsMaximised: vi.fn(() => Promise.resolve(false)),
  WindowMinimise: vi.fn(),
  WindowClose: vi.fn(),
  EventsOn: vi.fn(),
}));

vi.mock('./titlebar', () => ({
  dragRegion: { WebkitAppRegion: 'drag', '--wails-draggable': 'drag' },
  noDragRegion: { WebkitAppRegion: 'no-drag', '--wails-draggable': 'no-drag' },
}));

// 重依赖子组件：与对话框编排无关，替换为轻桩
vi.mock('./components/Sidebar', () => ({ default: () => null }));
vi.mock('./components/RequestEditor', () => ({ default: () => null }));
vi.mock('./components/ResponseViewer', () => ({ default: () => null }));
vi.mock('./components/WorkspaceSwitcher', () => ({ default: () => null }));
vi.mock('./components/EnvSwitcher', () => ({ default: () => null }));
vi.mock('./components/WindowControls', () => ({ default: () => null }));
vi.mock('./components/CommandPalette', () => ({ default: () => null }));

function renderApp() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <DialogProvider>
        <App />
      </DialogProvider>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  window.history.replaceState(null, '', '/');
  // 隔离 tab store：清空工作区会话（App 的 openBlank effect 会重建空白页）
  useTabs.setState({ sessions: {} });
});

describe('Windows 液态玻璃视觉预览', () => {
  it('默认组合使用 A 的晶透材质和 C 的上下分屏布局', () => {
    window.history.replaceState(null, '', '/?glass=1');
    const { container } = renderApp();
    const preview = container.querySelector('.glass-lab');

    expect(preview).toHaveAttribute('data-variant', 'aurora');
    expect(preview).toHaveClass('glass-layout-stacked');
    expect(screen.getByRole('button', { name: '方案 A+C：晶透' })).toHaveAttribute('aria-pressed', 'true');
  });

  it('只在预览入口展示，且不读取工作区或发送真实请求', () => {
    window.history.replaceState(null, '', '/?glass=1');
    renderApp();

    expect(screen.getByRole('group', { name: '液态玻璃方案选择' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '最小化（视觉示意）' })).toBeDisabled();
    expect(ipc.getDefaultWorkspace).not.toHaveBeenCalled();
    expect(ipc.sendRequest).not.toHaveBeenCalled();
  });

  it.each([
    ['方案 A+C：晶透', 'aurora'],
    ['方案 B：烟灰', 'midnight'],
    ['方案 C：雾白', 'titanium'],
  ])('可选择 %s 并更新所选状态', (name, variant) => {
    window.history.replaceState(null, '', '/?glass=1');
    const { container } = renderApp();
    const button = screen.getByRole('button', { name });
    fireEvent.click(button);

    expect(button).toHaveAttribute('aria-pressed', 'true');
    expect(container.querySelector('.glass-lab')).toHaveAttribute('data-variant', variant);
    expect(container.querySelector('.glass-lab')?.classList.contains('glass-layout-stacked')).toBe(variant !== 'midnight');
  });

  it('参数页签展示对应内容，清空后可重新模拟响应，且不会发起请求', () => {
    window.history.replaceState(null, '', '/?glass=1');
    renderApp();
    fireEvent.click(screen.getByRole('tab', { name: '请求体' }));
    expect(screen.getByText('此 GET 请求无需请求体')).toBeInTheDocument();

    const response = screen.getByTestId('glass-response-code');
    expect(JSON.parse(response.textContent ?? '')).toHaveProperty('data.items');
    fireEvent.click(screen.getByRole('button', { name: '清空示例响应' }));
    expect(screen.queryByTestId('glass-response-code')).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: '模拟发送' }));
    expect(screen.getByTestId('glass-response-code')).toBeInTheDocument();
    expect(ipc.sendRequest).not.toHaveBeenCalled();
  });
});

describe('App 启动对话框状态', () => {
  beforeEach(async () => {
    // 预热 CookieManager 懒加载块：缺席断言不能靠“还没 import 完”侥幸通过
    await import('./components/CookieManager');
  });

  it('启动后不自动弹出 Cookie 管理（回归：0e8fd53 丢失 showCookies 守卫）', async () => {
    renderApp();
    // 工具栏出现 = workspace 已解析（自动弹出守卫的同一提交里挂载）
    await screen.findByRole('button', { name: 'Cookies' });
    expect(document.querySelector('.app-liquid-glass')).toBeInTheDocument();
    await act(async () => {
      await new Promise((r) => setTimeout(r, 10));
    });
    expect(screen.queryByText('Cookie 管理')).not.toBeInTheDocument();
  });

  it('Cookies 按钮可打开 Cookie 管理，关闭按钮可关闭', async () => {
    renderApp();
    const btn = await screen.findByRole('button', { name: 'Cookies' });
    fireEvent.click(btn);
    expect(await screen.findByText('Cookie 管理')).toBeInTheDocument();
    // ModalFrame 的关闭按钮（aria-label=关闭）
    fireEvent.click(screen.getByRole('button', { name: '关闭' }));
    await waitFor(() => {
      expect(screen.queryByText('Cookie 管理')).not.toBeInTheDocument();
    });
  });
});
