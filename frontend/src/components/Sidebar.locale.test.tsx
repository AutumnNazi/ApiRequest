// Sidebar 是 memo 组件：locale 切换不改变 props，必须自行订阅 locale 才会重渲染。
import { render, screen, act } from '@testing-library/react';
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { DialogProvider } from './DialogProvider';
import Sidebar from './Sidebar';
import { useLocale } from '../i18n/locale';

const ipc = vi.hoisted(() => ({
  listNodes: vi.fn(() => Promise.resolve([])),
  getNode: vi.fn(() => Promise.resolve(null)),
  renameNode: vi.fn(() => Promise.resolve(null)),
  upsertNode: vi.fn((n: unknown) => Promise.resolve(n)),
  deleteNode: vi.fn(() => Promise.resolve(null)),
  moveNodes: vi.fn(() => Promise.resolve(null)),
  listHistory: vi.fn(() => Promise.resolve({ items: [], nextCursor: '', hasMore: false })),
  getHistory: vi.fn(() => Promise.resolve(null)),
  clearHistory: vi.fn(() => Promise.resolve(null)),
  exportData: vi.fn(() => Promise.resolve('')),
  exportMirror: vi.fn(() => Promise.resolve(null)),
  openNativeDirectory: vi.fn(() => Promise.resolve('')),
  newDefaultRequest: vi.fn(() => ({})),
  toAppError: vi.fn((e: unknown) => ({ kind: 'unknown', detail: String(e) })),
}));

vi.mock('../ipc', () => ipc);

beforeEach(() => useLocale.getState().setLocale('zh-CN'));
afterEach(() => useLocale.getState().setLocale('zh-CN'));

function renderSidebar() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <DialogProvider>
        <Sidebar workspaceId="ws1" />
      </DialogProvider>
    </QueryClientProvider>,
  );
}

describe('Sidebar 的 memo 边界与语言切换', () => {
  it('默认中文时渲染中文页签', () => {
    renderSidebar();
    expect(screen.getByText('集合')).toBeInTheDocument();
    expect(screen.getByText('历史')).toBeInTheDocument();
  });

  it('切到 en 后页签立即变英文（props 未变，依赖 locale 订阅）', () => {
    renderSidebar();
    expect(screen.getByText('集合')).toBeInTheDocument();

    act(() => useLocale.getState().setLocale('en'));

    expect(screen.queryByText('集合')).toBeNull();
    expect(screen.queryByText('历史')).toBeNull();
    expect(screen.getByText('Collections')).toBeInTheDocument();
    expect(screen.getByText('History')).toBeInTheDocument();
  });

  it('切回中文能还原', () => {
    renderSidebar();
    act(() => useLocale.getState().setLocale('en'));
    expect(screen.getByText('Collections')).toBeInTheDocument();

    act(() => useLocale.getState().setLocale('zh-CN'));
    expect(screen.getByText('集合')).toBeInTheDocument();
  });
});
