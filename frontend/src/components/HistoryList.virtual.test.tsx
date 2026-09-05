// 历史列表虚拟化：几千条历史只渲染可视窗口（roadmap 增量打磨项）
import { render, screen, fireEvent, within } from '@testing-library/react';
import { describe, expect, it, vi, beforeEach } from 'vitest';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { DialogProvider } from './DialogProvider';
import Sidebar from './Sidebar';
import { useLocale } from '../i18n/locale';

const TOTAL = 3000;
const PAGE = 50;

// 页码式无限查询：每页 50 条，共 3000 条
const ipc = vi.hoisted(() => ({
  listNodes: vi.fn(() => Promise.resolve([])),
  getNode: vi.fn(() => Promise.resolve(null)),
  renameNode: vi.fn(() => Promise.resolve(null)),
  upsertNode: vi.fn((n: unknown) => Promise.resolve(n)),
  deleteNode: vi.fn(() => Promise.resolve(null)),
  moveNodes: vi.fn(() => Promise.resolve(null)),
  listHistory: vi.fn(),
  getHistory: vi.fn(() => Promise.resolve(null)),
  clearHistory: vi.fn(() => Promise.resolve(null)),
  exportData: vi.fn(() => Promise.resolve('')),
  exportMirror: vi.fn(() => Promise.resolve(null)),
  openNativeDirectory: vi.fn(() => Promise.resolve('')),
  newDefaultRequest: vi.fn(() => ({})),
  toAppError: vi.fn((e: unknown) => ({ kind: 'unknown', detail: String(e) })),
}));

vi.mock('../ipc', () => ipc);

function pageItem(i: number) {
  return {
    id: `h-${i}`,
    method: i % 2 ? 'GET' : 'POST',
    url: `https://api.test/item/${i}`,
    status: 200,
    createdAt: Date.now() - i * 1000,
  };
}

beforeEach(() => {
  useLocale.getState().setLocale('zh-CN');
  ipc.listHistory.mockImplementation((_ws: string, opts: { cursor?: string }) => {
    const start = opts?.cursor ? Number(opts.cursor) : 0;
    const items = Array.from({ length: Math.min(PAGE, TOTAL - start) }, (_, k) => pageItem(start + k));
    const next = start + PAGE;
    return Promise.resolve({ items, nextCursor: next < TOTAL ? String(next) : '', hasMore: next < TOTAL });
  });
});

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

async function openHistoryPane() {
  const view = renderSidebar();
  fireEvent.click(screen.getByRole('button', { name: '历史' }));
  // 首页数据抵达
  await screen.findByText('https://api.test/item/0');
  return view;
}

describe('HistoryList virtualized viewport', () => {
  it('mounts a virtual scroller instead of rendering every loaded row', async () => {
    await openHistoryPane();
    // 首页 50 条已加载，但 DOM 只含可视窗口内的行（jsdom clientHeight=0 → 窗口极小）
    const scroller = document.querySelector('[data-virtual-scroller]') as HTMLElement;
    expect(scroller).not.toBeNull();
    const renderedRows = document.querySelectorAll('[title="点击重放"]');
    expect(renderedRows.length).toBeGreaterThan(0);
    expect(renderedRows.length).toBeLessThan(PAGE);
  });

  it('keeps the LoadMoreTrigger footer inside the virtual scroller', async () => {
    await openHistoryPane();
    const scroller = document.querySelector('[data-virtual-scroller]') as HTMLElement;
    expect(within(scroller).getByText('滚动加载更多')).toBeTruthy();
  });

  it('expands the window when the scroller reports a real viewport height', async () => {
    const view = await openHistoryPane();
    const scroller = document.querySelector('[data-virtual-scroller]') as HTMLElement;
    Object.defineProperty(scroller, 'clientHeight', { value: 600, configurable: true });
    (scroller as HTMLElement & { scrollTop: number }).scrollTop = 0;
    fireEvent.scroll(scroller);
    // 600/44 ≈ 14 行 + overscan；仍远小于已加载的 50 条
    const renderedRows = document.querySelectorAll('[title="点击重放"]');
    expect(renderedRows.length).toBeGreaterThan(14);
    expect(renderedRows.length).toBeLessThan(PAGE);
    expect(view).toBeTruthy();
  });
});
