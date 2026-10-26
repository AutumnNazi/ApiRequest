import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import App from './App';
import { DialogProvider } from './components/DialogProvider';
import { useTabs } from './stores/tabs';
import { useTheme } from './theme/store';
import { BUILTIN_THEMES, loadSnapshot } from './theme/themes';
const { readFileSync } = await vi.importActual<{ readFileSync: (path: string, encoding: 'utf8') => string }>('node:fs');

const workspaceStyles = readFileSync('src/style.css', 'utf8');
const themeStyles = readFileSync('src/theme/theme.css', 'utf8');

// App 级冒烟：真实渲染 App + CookieManager（对话框编排的真实缝隙），
// 重依赖子组件用轻桩替换，ipc 全量 mock（App 自身挂载路径 + CookieManager）。
const ipc = vi.hoisted(() => ({
  getDefaultWorkspace: vi.fn(() =>
    Promise.resolve({ id: 'ws-1', name: '测试工作区', type: 'local', createdAt: 0, updatedAt: 0 }),
  ),
  listWorkspaces: vi.fn(() => Promise.resolve([{ id: 'ws-1', name: '测试工作区', type: 'local', createdAt: 0, updatedAt: 0 }])),
  listEnvironments: vi.fn(() => Promise.resolve([{ id: 'env-1', workspaceId: 'ws-1', name: '测试环境', isActive: true, variables: [] }])),
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

const runtime = vi.hoisted(() => ({
  WindowMaximise: vi.fn(),
  WindowUnmaximise: vi.fn(),
  WindowIsMaximised: vi.fn(() => Promise.resolve(false)),
  WindowMinimise: vi.fn(),
  WindowToggleMaximise: vi.fn(),
  WindowClose: vi.fn(),
  EventsOn: vi.fn(),
}));

vi.mock('../wailsjs/runtime/runtime', () => runtime);

// 重依赖子组件：与对话框编排无关，替换为轻桩
vi.mock('./components/Sidebar', () => ({ default: () => null }));
vi.mock('./components/RequestEditor', () => ({ default: () => null }));
vi.mock('./components/ResponseViewer', () => ({ default: () => null }));
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
  runtime.WindowIsMaximised.mockResolvedValue(false);
  localStorage.clear();
  useTheme.setState({ custom: [] });
  useTheme.getState().select('daylight');
  // 隔离 tab store：清空工作区会话（App 的 openBlank effect 会重建空白页）
  useTabs.setState({ sessions: {} });
});

afterEach(() => {
  useTheme.setState({ custom: [] });
  useTheme.getState().select('daylight');
});

describe('Titlebar native drag boundaries', () => {
  let titlebarStyles: HTMLStyleElement;
  beforeEach(() => {
    titlebarStyles = document.createElement('style');
    titlebarStyles.textContent = workspaceStyles.match(/\.app-glass-titlebar :is\([^}]+\}/)?.[0] ?? '';
    document.head.append(titlebarStyles);
  });
  afterEach(() => titlebarStyles.remove());

  it('excludes actual controls, their icons and open menu padding from native drag', async () => {
    renderApp();
    const titlebar = await screen.findByRole('banner');
    await screen.findByText('测试工作区');
    fireEvent.click(screen.getByTitle('工作区'));
    const menu = titlebar.querySelector('.absolute')!;
    expect(menu).not.toBeNull();
    const targets = [menu, ...titlebar.querySelectorAll('button, button *, input, select, textarea')];
    for (const target of targets) {
      expect(getComputedStyle(target).getPropertyValue('--wails-draggable'), target.outerHTML).toBe('no-drag');
    }
  });

  it('does not treat two separate primary presses as a titlebar double-click', async () => {
    renderApp();
    const titlebar = await screen.findByRole('banner');
    fireEvent.mouseDown(titlebar, { button: 0, buttons: 1, detail: 1 });
    fireEvent.mouseUp(titlebar, { button: 0 });
    fireEvent.mouseDown(screen.getByText('ApiRequest'), { button: 0, buttons: 1, detail: 1 });
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 0)); });
    expect(runtime.WindowMaximise).not.toHaveBeenCalled();
    expect(runtime.WindowUnmaximise).not.toHaveBeenCalled();
  });

  it('keeps environment inputs and modal backdrop outside the native drag area', async () => {
    runtime.WindowIsMaximised.mockResolvedValue(true);
    renderApp();
    await screen.findByRole('button', { name: '还原' });
    await screen.findByText('测试环境');
    fireEvent.click(screen.getByRole('button', { name: '管理' }));
    const dialog = await screen.findByRole('dialog');
    const input = dialog.querySelector('input')!;
    expect(input).not.toBeNull();
    fireEvent.change(input, { target: { value: '编辑后的环境' } });
    expect(input).toHaveValue('编辑后的环境');
    for (const target of [dialog.parentElement!, dialog, input]) {
      expect(getComputedStyle(target).getPropertyValue('--wails-draggable')).toBe('no-drag');
      fireEvent.doubleClick(target, { button: 0 });
    }
    expect(runtime.WindowUnmaximise).not.toHaveBeenCalled();
    expect(runtime.WindowMaximise).not.toHaveBeenCalled();
  });

  it('maximises and restores blank double-clicks after native resize state changes', async () => {
    renderApp();
    const titlebar = await screen.findByRole('banner');
    fireEvent.doubleClick(titlebar, { button: 0 });
    expect(runtime.WindowMaximise).toHaveBeenCalledOnce();
    expect(runtime.WindowUnmaximise).not.toHaveBeenCalled();
    runtime.WindowIsMaximised.mockResolvedValue(true);
    fireEvent(window, new Event('resize'));
    await screen.findByRole('button', { name: '还原' });
    fireEvent.doubleClick(titlebar, { button: 0 });
    expect(runtime.WindowUnmaximise).toHaveBeenCalledOnce();
    runtime.WindowIsMaximised.mockResolvedValue(false);
    fireEvent(window, new Event('resize'));
    await screen.findByRole('button', { name: '最大化' });
    fireEvent.doubleClick(titlebar, { button: 0 });
    expect(runtime.WindowMaximise).toHaveBeenCalledTimes(2);
  });

  it.each([false, true])('ignores double-clicks on controls and nested icons when maximised=%s', async (maximised) => {
    runtime.WindowIsMaximised.mockResolvedValue(maximised);
    renderApp();
    const titlebar = await screen.findByRole('banner');
    await screen.findByRole('button', { name: maximised ? '还原' : '最大化' });
    await screen.findByText('测试工作区');
    fireEvent.click(screen.getByTitle('工作区'));
    const menu = titlebar.querySelector('.absolute')!;
    for (const target of [menu, ...titlebar.querySelectorAll('button, button *')]) {
      fireEvent.doubleClick(target, { button: 0 });
    }
    fireEvent.doubleClick(titlebar, { button: 2 });
    expect(runtime.WindowMaximise).not.toHaveBeenCalled();
    expect(runtime.WindowUnmaximise).not.toHaveBeenCalled();
  });

  it('leaves decorative and flexible titlebar space outside no-drag controls', async () => {
    renderApp();
    const titlebar = await screen.findByRole('banner');
    const search = screen.getByTitle('命令面板 (Ctrl+K)');
    const blankRegions = [titlebar, titlebar.querySelector('h1')!, titlebar.querySelector('.app-glass-context')!, search.parentElement!, screen.getByTitle('主题').parentElement!];
    for (const region of blankRegions) {
      expect(region.closest('[data-no-drag]')).toBeNull();
      expect(getComputedStyle(region).getPropertyValue('--wails-draggable')).not.toBe('no-drag');
    }
  });

  it.each([false, true])('keeps the native drag marker when maximised=%s', async (maximised) => {
    runtime.WindowIsMaximised.mockResolvedValue(maximised);
    renderApp();
    const titlebar = await screen.findByRole('banner');
    await act(async () => {});
    expect(getComputedStyle(titlebar).getPropertyValue('--wails-draggable')).toBe('drag');
    fireEvent.mouseDown(titlebar, { button: 0, buttons: 1, detail: 1 });
    expect(runtime.WindowMaximise).not.toHaveBeenCalled();
    expect(runtime.WindowUnmaximise).not.toHaveBeenCalled();
  });
});

describe('Workspace theme material', () => {
  it('disables sidebar width transitions only in the small-screen overlay layout', () => {
    const mobileStyles = workspaceStyles.slice(workspaceStyles.indexOf('@media (max-width: 760px)'), workspaceStyles.indexOf('@media (prefers-reduced-motion: reduce)'));
    expect(mobileStyles).toMatch(/\.app-liquid-glass \.app-glass-sidebar-shell\s*\{\s*transition:\s*none;\s*\}/);
  });

  it('keeps a solid theme canvas and edge-only glass without decorative backgrounds', () => {
    const root = workspaceStyles.match(/\.app-liquid-glass\s*\{([^}]+)\}/)?.[1];
    expect(root).toContain('background: var(--t-bg-app)');
    expect(root).toContain('background-image: none');
    expect(workspaceStyles).toContain('background: var(--liquid-surface, var(--liquid-pane))');
    expect(workspaceStyles).toContain('box-shadow: var(--liquid-bevel)');
    expect(workspaceStyles + themeStyles).not.toMatch(/wallpaper|refraction|ribbon|fold|liquid-sheen|liquid-rim/);
    const dialog = themeStyles.slice(themeStyles.indexOf('.app-dialog-surface {'));
    expect(dialog).toContain('background-color: Canvas !important');
    expect(dialog).toContain('linear-gradient(var(--t-bg-panel), var(--t-bg-panel)), linear-gradient(var(--t-bg-app), var(--t-bg-app))');
    expect(dialog).toContain('backdrop-filter: none');
    expect(dialog).not.toContain('--liquid-bevel');
  });

  it('joins tabs and panes without card gutters while keeping control insets', async () => {
    const { container } = renderApp();
    await screen.findByRole('button', { name: 'Cookies' });
    const main = container.querySelector('main')!;
    expect(main.children[0]).toHaveClass('app-glass-tabs');
    expect(main.children[1]).toHaveClass('app-glass-split');
    expect(workspaceStyles).toMatch(/\.app-glass-split\s*\{\s*padding:\s*0;\s*\}/);
    expect(workspaceStyles).toMatch(/\.app-glass-tabs\s*\{[^}]*padding-inline:\s*10px;/);
    expect(workspaceStyles).toMatch(/\.app-glass-response-pane\s*\{[^}]*border:\s*0;[^}]*border-radius:\s*0;/);
    expect(container.querySelector('.app-glass-split')).toHaveAttribute('data-layout', 'horizontal');
    expect(container.querySelector('.app-glass-split > .app-splitter')).toHaveAttribute('data-orientation', 'horizontal');
    fireEvent.click(screen.getByTitle('响应全屏禅模式'));
    expect(container.querySelector('.app-glass-request-pane')).toBeNull();
    expect(container.querySelector('.app-glass-split > .app-splitter')).toBeNull();
    fireEvent.click(screen.getByTitle('退出全屏禅模式'));
    expect(container.querySelector('.app-glass-request-pane')).not.toBeNull();
  });

  it.each(['daylight', 'comfort-dark', 'midnight-navy', 'nord-slate', 'deep-ocean', 'warm-paper', 'mist-jade'])('keeps %s workspace glass on its own solid theme canvas', async (id) => {
      const { container } = renderApp();
      await screen.findByRole('button', { name: 'Cookies' });
      act(() => useTheme.getState().select(id));
      for (const className of [
        'app-liquid-glass', 'app-glass-titlebar', 'app-glass-sidebar-shell',
        'app-glass-tabs', 'app-glass-request-pane', 'app-glass-response-pane',
      ]) {
        expect(container.querySelector('.' + className)).not.toBeNull();
      }
      expect(document.documentElement.dataset.material).toBe('liquid-glass');
      expect(document.documentElement.dataset.themeId).toBe(id);
      expect(container.querySelector('.glass-wallpaper, #liquid-refraction, #glass-ribbon, #glass-fold')).toBeNull();
      expect(document.documentElement.style.getPropertyValue('--t-bg-app')).toBe(BUILTIN_THEMES.find((theme) => theme.id === id)!.palette.bgApp);
    });

  it('keeps editable copies glass without a palette material flag', async () => {
    const { container } = renderApp();
    await screen.findByRole('button', { name: 'Cookies' });
    let id = '';
    act(() => {
      id = useTheme.getState().duplicate('comfort-dark', 'Custom glass')!;
      useTheme.getState().select(id);
    });
    expect(document.documentElement.dataset.theme).toBe('dark');
    expect(document.documentElement.dataset.material).toBe('liquid-glass');
    expect(container.querySelector('.app-liquid-glass')).not.toBeNull();
    expect(loadSnapshot().custom.find((theme) => theme.id === id)?.palette.material).toBeUndefined();
  });

  it.each([undefined, 'liquid-glass'] as const)('keeps an old custom palette glass with material %s', async (material) => {
    const custom = { id: 'custom-legacy', name: 'Legacy', builtin: false, palette: { ...BUILTIN_THEMES.find((t) => t.id === 'warm-paper')!.palette, ...(material ? { material } : {}) } };
    localStorage.setItem('apirequest-themes-v1', JSON.stringify({ version: 1, activeId: custom.id, custom: [custom] }));
    useTheme.setState(loadSnapshot());
    useTheme.getState().select(custom.id);
    const { container } = renderApp();
    await screen.findByRole('button', { name: 'Cookies' });
    expect(document.documentElement.dataset.material).toBe('liquid-glass');
    expect(container.querySelector('.app-liquid-glass')).not.toBeNull();
    expect(container.querySelector('.glass-wallpaper')).toBeNull();
  });

  it('preserves custom glass colors and mode when editing', async () => {
    const { container } = renderApp();
    await screen.findByRole('button', { name: 'Cookies' });
    act(() => {
      const id = useTheme.getState().duplicate('daylight', 'Dark glass')!;
      useTheme.getState().select(id);
      useTheme.getState().updateCustom(id, {
        palette: { mode: 'dark', accent: '#70c9ba', bgApp: '#101821', bgPanel: 'rgba(32,49,60,0.8)' },
      });
    });
    expect(document.documentElement.dataset.theme).toBe('dark');
    expect(document.documentElement.dataset.material).toBe('liquid-glass');
    expect(document.documentElement.style.getPropertyValue('--t-accent')).toBe('#70c9ba');
    expect(document.documentElement.style.getPropertyValue('--t-bg-panel')).toBe('rgba(32,49,60,0.8)');
    expect(document.documentElement.style.getPropertyValue('--t-bg-app')).toBe('#101821');
    expect(container.querySelector('.app-liquid-glass')).not.toBeNull();
    expect(container.querySelector('#liquid-refraction')).toBeNull();
  });

  it('keeps the chosen split layout when switching colors and modes', async () => {
    const { container } = renderApp();
    await screen.findByRole('button', { name: 'Cookies' });
    fireEvent.click(screen.getByTitle('切换为左右并排分屏'));
    expect(container.querySelector('.app-glass-split')).toHaveAttribute('data-layout', 'vertical');
    act(() => useTheme.getState().select('warm-paper'));
    expect(container.querySelector('.app-glass-split')).toHaveAttribute('data-layout', 'vertical');
    act(() => useTheme.getState().select('midnight-navy'));
    expect(container.querySelector('.app-glass-split')).toHaveAttribute('data-layout', 'vertical');
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
    fireEvent.click(screen.getByRole('dialog').querySelector('button[aria-label="关闭"]')!);
    await waitFor(() => {
      expect(screen.queryByText('Cookie 管理')).not.toBeInTheDocument();
    });
  });
});

describe('Theme snapshot migration', () => {
  it('offers only the seven retained builtins and defaults to daylight', () => {
    localStorage.clear();
    expect(BUILTIN_THEMES.map((theme) => theme.id)).toEqual([
      'daylight', 'comfort-dark', 'midnight-navy', 'nord-slate', 'deep-ocean', 'warm-paper', 'mist-jade',
    ]);
    expect(loadSnapshot().activeId).toBe('daylight');
  });

  it.each(['liquid-glass', 'missing-theme', ''])('normalizes restored active ID %s before store initialization and saving', async (activeId) => {
    localStorage.setItem('apirequest-themes-v1', JSON.stringify({ version: 1, activeId, custom: [] }));
    expect(loadSnapshot().activeId).toBe('daylight');
    vi.resetModules();
    const { useTheme: restored } = await import('./theme/store');
    expect(restored.getState().activeId).toBe('daylight');
    expect(restored.getState().active().id).toBe('daylight');
    restored.getState().duplicate('daylight', 'Restored copy');
    expect(JSON.parse(localStorage.getItem('apirequest-themes-v1')!).activeId).toBe('daylight');
  });

  it('reserves the deleted builtin ID without removing legitimate saved custom themes', () => {
    const palette = BUILTIN_THEMES.find((theme) => theme.id === 'daylight')!.palette;
    const custom = [
      { id: 'liquid-glass', name: 'Deleted builtin collision', builtin: false, palette },
      { id: 'custom-retained', name: 'Retained custom', builtin: false, palette },
    ];
    localStorage.setItem('apirequest-themes-v1', JSON.stringify({ version: 1, activeId: 'liquid-glass', custom }));
    expect(loadSnapshot()).toMatchObject({ activeId: 'daylight', custom: [custom[1]] });
    localStorage.setItem('apirequest-themes-v1', JSON.stringify({ version: 1, activeId: 'custom-retained', custom }));
    expect(loadSnapshot()).toMatchObject({ activeId: 'custom-retained', custom: [custom[1]] });
  });

  it('returns to daylight and saves the normalized ID after removing the active custom theme', () => {
    const id = useTheme.getState().duplicate('comfort-dark', 'Removable')!;
    useTheme.getState().select(id);
    useTheme.getState().removeCustom(id);
    expect(useTheme.getState().activeId).toBe('daylight');
    expect(loadSnapshot().activeId).toBe('daylight');
    expect(document.documentElement.dataset.material).toBe('liquid-glass');
  });
});
