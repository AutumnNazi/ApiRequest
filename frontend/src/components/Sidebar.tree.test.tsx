import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeAll, describe, expect, it, vi, beforeEach } from 'vitest';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { DialogProvider } from './DialogProvider';
import Sidebar from './Sidebar';
import type { NodeSummary } from '../ipc';
import { useTabs } from '../stores/tabs';

const ipc = vi.hoisted(() => ({
  listNodes: vi.fn((_workspaceId?: string) => Promise.resolve([] as NodeSummary[])),
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
  toAppError: vi.fn((e: unknown) => ({
    kind: 'unknown',
    detail: e instanceof Error ? e.message : String(e),
  })),
}));

vi.mock('../ipc', () => ipc);
vi.mock('./RunnerDialog', () => ({ default: ({ collectionId }: { collectionId: string }) => <div role="dialog">Runner {collectionId}</div> }));
vi.mock('./MockPanel', () => ({ default: ({ collectionId }: { collectionId: string }) => <div role="dialog">Mock {collectionId}</div> }));

const nodes = [
  { id: 'c1', workspaceId: 'ws1', parentId: null, kind: 'collection', name: '集合1', sortOrder: 0, createdAt: 0, updatedAt: 0 },
  { id: 'r1', workspaceId: 'ws1', parentId: 'c1', kind: 'request', name: '请求1', method: 'GET', sortOrder: 0, createdAt: 0, updatedAt: 0 },
  { id: 'r2', workspaceId: 'ws1', parentId: 'c1', kind: 'request', name: '请求2', method: 'POST', sortOrder: 1, createdAt: 0, updatedAt: 0 },
  { id: 'r3', workspaceId: 'ws1', parentId: 'c1', kind: 'request', name: '请求3', method: 'PUT', sortOrder: 2, createdAt: 0, updatedAt: 0 },
] as NodeSummary[];

function renderTree(workspaceId = 'ws1') {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const view = render(
    <QueryClientProvider client={qc}>
      <DialogProvider>
        <Sidebar workspaceId={workspaceId} />
      </DialogProvider>
    </QueryClientProvider>,
  );
  return {
    ...view,
    rerenderWorkspace(nextWorkspaceId: string) {
      view.rerender(
        <QueryClientProvider client={qc}>
          <DialogProvider>
            <Sidebar workspaceId={nextWorkspaceId} />
          </DialogProvider>
        </QueryClientProvider>,
      );
    },
  };
}

beforeAll(() => {
  // 虚拟化列表依赖可视高度；jsdom 全部为 0，会只渲染 1 行。统一注入 600px。
  Object.defineProperty(HTMLElement.prototype, 'clientHeight', {
    configurable: true,
    get: () => 600,
  });
});

beforeEach(() => {
  vi.clearAllMocks();
  ipc.listNodes.mockReset();
  ipc.listNodes.mockResolvedValue(nodes);
  ipc.listHistory.mockReset();
  ipc.listHistory.mockResolvedValue({ items: [], nextCursor: '', hasMore: false });
  ipc.upsertNode.mockReset();
  ipc.upsertNode.mockImplementation((node: unknown) => Promise.resolve(node));
  ipc.deleteNode.mockResolvedValue(null);
  ipc.clearHistory.mockResolvedValue(null);
  ipc.moveNodes.mockResolvedValue(null);
  ipc.openNativeDirectory.mockResolvedValue('');
  ipc.exportData.mockResolvedValue('exported data');
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: vi.fn().mockResolvedValue(undefined) } });
  useTabs.setState({ sessions: {} });
});

describe('CollectionTree folder icons', () => {
  it('keeps the full tree row compact with separate trailing counts and one more action', async () => {
    ipc.listNodes.mockResolvedValue([
      { ...nodes[0], name: '集合名称'.repeat(30) },
      { ...nodes[0], id: 'f1', parentId: 'c1', kind: 'folder', name: '子文件夹' },
      { ...nodes[1], parentId: 'f1' },
    ]);
    const view = renderTree();
    await screen.findByText('请求1');
    for (const [id, depth, weight] of [['c1', 0, 'font-semibold'], ['f1', 1, 'font-medium'], ['r1', 2, '']] as const) {
      const row = view.container.querySelector(`[data-node-id="${id}"]`) as HTMLElement;
      expect(row).toHaveClass('text-[11px]', 'leading-4', 'h-full');
      expect(row.parentElement).toHaveStyle({ height: '24px', top: `${depth * 24}px` });
      expect(row).toHaveStyle({ paddingLeft: `${depth * 14 + (id === 'r1' ? 4 : 0)}px` });
      const name = row.querySelector('.truncate') as HTMLElement;
      expect(name).toHaveClass('min-w-0', 'flex-1');
      const buttons = row.querySelectorAll('button');
      expect(buttons).toHaveLength(id === 'r1' ? 1 : 2);
      const more = buttons[buttons.length - 1];
      expect(more).toHaveAccessibleName();
      expect(more).toHaveAttribute('aria-haspopup', 'menu');
      expect(more).toHaveClass('group-hover:opacity-100', 'focus-visible:opacity-100');
      if (weight) {
        expect(name).toHaveClass(weight);
        expect(name).not.toHaveTextContent('1');
        const count = name.nextElementSibling;
        expect(count).toHaveTextContent('1');
        expect(count).toHaveClass('shrink-0', 'text-[10px]');
        expect(row.querySelector('svg')).toHaveClass('shrink-0');
      }
    }
  });
  it.each(['collection', 'folder'] as const)('renders themed SVG states and preserves %s actions', async (kind) => {
    ipc.listNodes.mockResolvedValue([
      nodes[0],
      { ...nodes[0], id: 'f1', parentId: 'c1', kind: 'folder', name: '子文件夹' },
      { ...nodes[1], parentId: 'f1' },
    ]);
    const view = renderTree();
    const nameCell = await screen.findByText(kind === 'collection' ? /集合1/ : /子文件夹/);
    const row = nameCell.closest('[data-node-id]') as HTMLElement;
    const nodeId = kind === 'collection' ? 'c1' : 'f1';
    const expander = row.querySelector('button[title="展开"], button[title="折叠"]') as HTMLButtonElement;
    if (expander.title === '展开') fireEvent.click(expander);

    const openIcon = row.querySelector('svg');
    expect(openIcon).not.toBeNull();
    expect(expander).toHaveClass('mr-0', 'w-4');
    expect(row).toHaveStyle({ paddingLeft: kind === 'collection' ? '0px' : '14px' });
    expect(openIcon).toHaveAttribute('fill', 'currentColor');
    expect(openIcon).not.toHaveAttribute('stroke');
    expect(openIcon).toHaveAttribute('viewBox', '0 -960 960 960');
    expect(openIcon?.querySelector('path')).toHaveAttribute('d', 'M160-160q-33 0-56.5-23.5T80-240v-480q0-33 23.5-56.5T160-800h240l80 80h320q33 0 56.5 23.5T880-640H447l-80-80H160v480l96-320h684L837-217q-8 26-29.5 41.5T760-160H160Zm84-80h516l72-240H316l-72 240Zm0 0 72-240-72 240Zm-84-400v-80 80Z');
    expect(openIcon).toHaveClass('mr-1.5', kind === 'collection' ? 'text-blue-600' : 'text-gray-500');
    expect(openIcon).toHaveAttribute('aria-hidden', 'true');
    expect(openIcon).toHaveAttribute('focusable', 'false');
    const size = kind === 'collection' ? '14' : '13';
    expect(openIcon).toHaveAttribute('width', size);
    expect(openIcon).toHaveAttribute('height', size);
    const openShape = openIcon?.innerHTML;
    expect(nameCell.nextElementSibling).toHaveTextContent('1');
    expect(view.container).not.toHaveTextContent(/📁|📂/);

    const request = (await screen.findByText('请求1')).closest('[data-node-id]') as HTMLElement;
    expect(request).toHaveTextContent('GET');
    fireEvent.mouseDown(request);
    fireEvent.click(request);
    fireEvent.click(expander);
    expect(screen.getByText('已选 1 项')).toBeInTheDocument();
    const closedIcon = row.querySelector('svg');
    const closedShape = closedIcon?.innerHTML;
    expect(closedIcon?.querySelector('path')).toHaveAttribute('d', 'M160-160q-33 0-56.5-23.5T80-240v-480q0-33 23.5-56.5T160-800h240l80 80h320q33 0 56.5 23.5T880-640v400q0 33-23.5 56.5T800-160H160Zm0-80h640v-400H447l-80-80H160v480Zm0 0v-480 480Z');
    expect(closedIcon).toHaveClass(kind === 'collection' ? 'text-gray-400' : 'text-gray-500');
    expect(closedShape).toBeTruthy();
    expect(closedShape).not.toBe(openShape);
    fireEvent.click(expander);
    expect(row.querySelector('svg')?.innerHTML).toBe(openShape);

    fireEvent.click(within(row).getByRole('button', { name: /操作/ }));
    const addFolder = screen.getByRole('button', { name: kind === 'collection' ? '添加文件夹' : '添加子文件夹' });
    expect(addFolder).toHaveAccessibleName(kind === 'collection' ? '添加文件夹' : '添加子文件夹');
    const addIcon = addFolder.querySelector('svg');
    expect(addIcon).toHaveAttribute('fill', 'currentColor');
    expect(addIcon).not.toHaveAttribute('stroke');
    expect(addIcon?.querySelector('path')).toHaveAttribute('d', 'M560-320h80v-80h80v-80h-80v-80h-80v80h-80v80h80v80ZM160-160q-33 0-56.5-23.5T80-240v-480q0-33 23.5-56.5T160-800h240l80 80h320q33 0 56.5 23.5T880-640v400q0 33-23.5 56.5T800-160H160Zm0-80h640v-400H447l-80-80H160v480Zm0 0v-480 480Z');
    expect(addIcon).toHaveAttribute('aria-hidden', 'true');
    expect(addIcon).toHaveAttribute('focusable', 'false');
    expect(addIcon?.innerHTML).not.toBe(closedShape);
    fireEvent.mouseOver(row);
    fireEvent.click(addFolder);
    await waitFor(() => expect(ipc.upsertNode).toHaveBeenCalledWith(expect.objectContaining({ parentId: nodeId, kind: 'folder' })));
    expect(screen.getByText('已选 1 项')).toBeInTheDocument();
  });
});

describe('CollectionTree multi-select', () => {
  async function req(id: string) {
    await screen.findByText('请求1');
    return screen.getByText(id).closest('[data-node-id]') as HTMLElement;
  }

  // 选择在 mousedown 完成；模拟真实手势：mousedown + click 都带同样的修饰键
  function gesture(el: HTMLElement, mods: { ctrlKey?: boolean; shiftKey?: boolean }) {
    fireEvent.mouseDown(el, mods);
    fireEvent.click(el, mods);
  }

  it('plain click selects a single node and shows the batch bar', async () => {
    renderTree();
    const req1 = await req('请求1');
    gesture(req1, {});
    expect(screen.getByText('已选 1 项')).toBeInTheDocument();
  });

  it('keeps single and multiple selection actions in one compact row', async () => {
    renderTree();
    gesture(await req('请求1'), {});
    const bar = screen.getByRole('group', { name: '操作' });
    const count = within(bar).getByText('已选 1 项');
    expect(bar).toHaveClass('flex-nowrap', 'shrink-0');
    expect(count).toHaveClass('whitespace-nowrap', 'tabular-nums');
    expect(bar.querySelector('kbd, .animate-pulse')).toBeNull();
    const remove = within(bar).getByRole('button', { name: '删除' });
    const cancel = within(bar).getByRole('button', { name: '取消选择' });
    expect(remove).toHaveClass('shrink-0', 'whitespace-nowrap');
    expect(cancel).toHaveClass('shrink-0', 'whitespace-nowrap');
    expect(cancel).toHaveTextContent('取消');
    fireEvent.click(cancel);
    expect(screen.queryByRole('group', { name: '操作' })).toBeNull();
    gesture(await req('请求1'), {});
    gesture(await req('请求2'), { ctrlKey: true });
    expect(screen.getByRole('group', { name: '操作' })).toHaveTextContent('已选 2 项');
    fireEvent.click(screen.getByRole('button', { name: '删除' }));
    expect(await screen.findByRole('dialog')).toBeInTheDocument();
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: '取消' }));
    expect(ipc.deleteNode).not.toHaveBeenCalled();
    expect(screen.getByRole('group', { name: '操作' })).toHaveTextContent('已选 2 项');
  });

  it('plain click moves selection to the clicked node', async () => {
    renderTree();
    const req1 = await req('请求1');
    const req2 = screen.getByText('请求2').closest('[data-node-id]') as HTMLElement;

    gesture(req1, {});
    gesture(req2, {});
    expect(screen.getByText('已选 1 项')).toBeInTheDocument();
  });

  it('ctrl+click toggles and accumulates selection', async () => {
    renderTree();
    const req1 = await req('请求1');
    const req2 = screen.getByText('请求2').closest('[data-node-id]') as HTMLElement;

    gesture(req1, { ctrlKey: true });
    expect(screen.getByText('已选 1 项')).toBeInTheDocument();

    gesture(req2, { ctrlKey: true });
    expect(screen.getByText('已选 2 项')).toBeInTheDocument();
  });

  it('ctrl+click again deselects a node', async () => {
    renderTree();
    const req1 = await req('请求1');
    gesture(req1, { ctrlKey: true });
    gesture(req1, { ctrlKey: true });
    expect(screen.queryByText('已选 1 项')).not.toBeInTheDocument();
  });

  it('shift+click selects a range', async () => {
    renderTree();
    const req1 = await req('请求1');
    const req3 = screen.getByText('请求3').closest('[data-node-id]') as HTMLElement;

    gesture(req1, { ctrlKey: true });
    gesture(req3, { shiftKey: true });
    expect(screen.getByText('已选 3 项')).toBeInTheDocument();
  });

  it('repeated shift+click extends range from the original anchor', async () => {
    renderTree();
    const req1 = await req('请求1');
    const req2 = screen.getByText('请求2').closest('[data-node-id]') as HTMLElement;
    const req3 = screen.getByText('请求3').closest('[data-node-id]') as HTMLElement;

    gesture(req1, { ctrlKey: true });
    gesture(req2, { shiftKey: true });
    expect(screen.getByText('已选 2 项')).toBeInTheDocument();

    // 锚点仍是 req1：Shift 到 req3 应得到 r1..r3 三项，而不是从 req2 重新起算的两项
    gesture(req3, { shiftKey: true });
    expect(screen.getByText('已选 3 项')).toBeInTheDocument();
  });

  it('plain click after multi-select narrows to the clicked node', async () => {
    renderTree();
    const req1 = await req('请求1');
    const req2 = screen.getByText('请求2').closest('[data-node-id]') as HTMLElement;
    const req3 = screen.getByText('请求3').closest('[data-node-id]') as HTMLElement;

    gesture(req1, { ctrlKey: true });
    gesture(req2, { ctrlKey: true });
    expect(screen.getByText('已选 2 项')).toBeInTheDocument();

    gesture(req3, {});
    expect(screen.getByText('已选 1 项')).toBeInTheDocument();
  });

  it('double-click opens the request without toggling selection', async () => {
    ipc.getNode.mockReset();
    ipc.getNode.mockResolvedValue({
      workspaceId: 'ws1',
      id: 'r1',
      name: '请求1',
      request: { method: 'GET', url: 'http://example.com', headers: [], params: [], body: { kind: 'none' } },
    } as never);
    renderTree();
    const req1 = await req('请求1');
    gesture(req1, {});

    fireEvent.doubleClick(req1, {});
    expect(ipc.getNode).toHaveBeenCalledWith('ws1', 'r1');
    expect(screen.getByText('已选 1 项')).toBeInTheDocument();
  });

  it('shift+click without an anchor selects the node and anchors range', async () => {
    renderTree();
    const req1 = await req('请求1');
    const req3 = screen.getByText('请求3').closest('[data-node-id]') as HTMLElement;

    gesture(req1, { shiftKey: true });
    expect(screen.getByText('已选 1 项')).toBeInTheDocument();

    gesture(req3, { shiftKey: true });
    expect(screen.getByText('已选 3 项')).toBeInTheDocument();
  });

  it('folder expander button toggles without changing selection', async () => {
    renderTree();
    const req1 = await req('请求1');
    const req2 = screen.getByText('请求2').closest('[data-node-id]') as HTMLElement;

    gesture(req1, { ctrlKey: true });
    gesture(req2, { ctrlKey: true });
    expect(screen.getByText('已选 2 项')).toBeInTheDocument();

    const expander = screen.getByTitle('折叠');
    fireEvent.click(expander, {});
    expect(screen.getByText('已选 2 项')).toBeInTheDocument();
  });

  it('Shift+ArrowDown extends selection from the anchor', async () => {
    const view = renderTree();
    const req1 = await req('请求1');
    gesture(req1, {});
    expect(screen.getByText('已选 1 项')).toBeInTheDocument();

    const tree = view.container.querySelector('div[tabindex="0"]') as HTMLElement;
    fireEvent.keyDown(tree, { key: 'ArrowDown', shiftKey: true });
    expect(screen.getByText('已选 2 项')).toBeInTheDocument();

    fireEvent.keyDown(tree, { key: 'ArrowDown', shiftKey: true });
    expect(screen.getByText('已选 3 项')).toBeInTheDocument();
  });

  it('Space toggles the focused node in the selection', async () => {
    const view = renderTree();
    const req1 = await req('请求1');
    gesture(req1, {});
    expect(screen.getByText('已选 1 项')).toBeInTheDocument();

    const tree = view.container.querySelector('div[tabindex="0"]') as HTMLElement;
    fireEvent.keyDown(tree, { key: ' ' });
    expect(screen.queryByText('已选 1 项')).not.toBeInTheDocument();

    fireEvent.keyDown(tree, { key: ' ' });
    expect(screen.getByText('已选 1 项')).toBeInTheDocument();
  });

  it('clears selection when switching workspaces', async () => {
    ipc.listNodes.mockImplementation((workspaceId = 'ws1') =>
      Promise.resolve(workspaceId === 'ws1' ? nodes : []),
    );
    const view = renderTree();
    gesture(await req('请求1'), {});
    expect(screen.getByText('已选 1 项')).toBeInTheDocument();

    view.rerenderWorkspace('ws2');
    await waitFor(() => expect(screen.queryByText('已选 1 项')).not.toBeInTheDocument());
  });

  it('passes workspace ownership to delete', async () => {
    const view = renderTree();
    gesture(await req('请求1'), {});
    const tree = view.container.querySelector('div[tabindex="0"]') as HTMLElement;
    fireEvent.keyDown(tree, { key: 'Delete' });
    fireEvent.click(await screen.findByRole('button', { name: '确定' }));

    await waitFor(() => expect(ipc.deleteNode).toHaveBeenCalledWith('ws1', 'r1'));
  });

  it('does not detach tabs while the node query is still loading', async () => {
    let resolveNodes!: (value: NodeSummary[]) => void;
    ipc.listNodes.mockImplementationOnce(() => new Promise<NodeSummary[]>((resolve) => {
      resolveNodes = resolve;
    }));
    const tabId = useTabs.getState().openNode('ws1', 'r1', '请求1', {} as never);

    renderTree();
    await waitFor(() => expect(ipc.listNodes).toHaveBeenCalledWith('ws1'));
    expect(useTabs.getState().sessions.ws1.tabs.find((tab) => tab.id === tabId)?.nodeId).toBe('r1');

    resolveNodes(nodes);
    await screen.findByText('请求1');
    expect(useTabs.getState().sessions.ws1.tabs.find((tab) => tab.id === tabId)?.nodeId).toBe('r1');
  });

  it('detaches an open tab after a successful node refresh confirms deletion', async () => {
    const tabId = useTabs.getState().openNode('ws1', 'missing-request', '已删除请求', {} as never);

    renderTree();
    await screen.findByText('请求1');
    await waitFor(() => {
      const tab = useTabs.getState().sessions.ws1.tabs.find((candidate) => candidate.id === tabId);
      expect(tab?.nodeId).toBeUndefined();
      expect(tab?.dirty).toBe(true);
    });
  });

  it('deletes only top-level selections when a parent and child are both selected', async () => {
    const view = renderTree();
    await screen.findByText('请求1');
    const collection = view.container.querySelector('[data-node-id="c1"]') as HTMLElement;
    gesture(collection, { ctrlKey: true });
    gesture(await req('请求1'), { ctrlKey: true });
    expect(screen.getByText('已选 2 项')).toBeInTheDocument();

    const tree = view.container.querySelector('div[tabindex="0"]') as HTMLElement;
    fireEvent.keyDown(tree, { key: 'Delete' });
    fireEvent.click(await screen.findByRole('button', { name: '确定' }));

    await waitFor(() => expect(ipc.deleteNode).toHaveBeenCalledTimes(1));
    expect(ipc.deleteNode).toHaveBeenCalledWith('ws1', 'c1');
  });

  it('moves every selected request when dragging into another collection', async () => {
    ipc.listNodes.mockResolvedValue([
      ...nodes,
      { id: 'c2', workspaceId: 'ws1', parentId: null, kind: 'collection', name: '集合2', sortOrder: 1, createdAt: 0, updatedAt: 0 },
    ] as NodeSummary[]);
    const view = renderTree();
    const req1 = await req('请求1');
    const req2 = screen.getByText('请求2').closest('[data-node-id]') as HTMLElement;
    gesture(req1, { ctrlKey: true });
    gesture(req2, { ctrlKey: true });

    const dataTransfer = { effectAllowed: '', setData: vi.fn() };
    fireEvent.dragStart(req1, { dataTransfer });
    const target = view.container.querySelector('[data-node-id="c2"]') as HTMLElement;
    fireEvent.dragOver(target, { dataTransfer });
    fireEvent.drop(target, { dataTransfer });

    await waitFor(() => expect(ipc.moveNodes).toHaveBeenCalledOnce());
    expect(ipc.moveNodes).toHaveBeenCalledWith('ws1', [
      { id: 'r1', parentId: 'c2', sortOrder: 0 },
      { id: 'r2', parentId: 'c2', sortOrder: 1 },
    ]);
  });

  it('reports collection creation failures', async () => {
    ipc.upsertNode.mockRejectedValueOnce(new Error('disk full'));
    renderTree();
    await screen.findByText('请求1');

    fireEvent.click(screen.getByRole('button', { name: /新建集合/ }));

    expect(await screen.findByRole('alertdialog')).toHaveTextContent('创建集合失败: disk full');
  });

  it('shows history load failures and allows retrying', async () => {
    ipc.listHistory.mockRejectedValueOnce(new Error('database offline'));
    renderTree();

    fireEvent.click(screen.getByRole('button', { name: '历史' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('加载历史失败：database offline');
    fireEvent.click(screen.getByRole('button', { name: '重试' }));
    await waitFor(() => expect(ipc.listHistory).toHaveBeenCalledTimes(2));
  });
});

describe('CollectionTree virtualization', () => {
  it('bounds the tree viewport through its flex height chain without a second collection scroller', async () => {
    renderTree();
    const row = (await screen.findByText('请求1')).closest('[data-node-id]') as HTMLElement;
    const scroller = row.closest('[data-virtual-scroller]') as HTMLElement;
    const tree = scroller.parentElement as HTMLElement;
    const collection = tree.parentElement as HTMLElement;
    const pane = collection.parentElement as HTMLElement;
    expect(collection).toHaveClass('h-full', 'min-h-0');
    expect(collection).not.toHaveClass('min-h-full');
    expect(tree).toHaveClass('min-h-0', 'flex-1');
    expect(pane).toHaveClass('min-h-0', 'overflow-hidden');
    expect(pane).not.toHaveClass('overflow-auto');
    fireEvent.mouseDown(row);
    fireEvent.click(row);
    expect(screen.getByText('已选 1 项')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: '历史' }));
    expect(pane).toHaveClass('overflow-auto');
    expect(pane).not.toHaveClass('overflow-hidden');
  });

  it('keeps only a window of rows in the DOM for large trees and scrolls to the tail', async () => {
    const many = Array.from({ length: 60 }, (_, i) => ({
      id: `col-big-${i}`,
      workspaceId: 'ws-1',
      parentId: '',
      kind: 'collection',
      name: `bulk ${i}`,
      sortOrder: i,
      createdAt: i,
    })) as unknown as NodeSummary[];
    ipc.listNodes.mockResolvedValue(many);
    const view = render(
      <DialogProvider>
        <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
          <Sidebar workspaceId="ws-1" />
        </QueryClientProvider>
      </DialogProvider>,
    );
    await waitFor(() => expect(view.container.querySelector('[data-node-id="col-big-0"]')).not.toBeNull());

    const inDom = view.container.querySelectorAll('[data-node-id]').length;
    expect(inDom).toBeGreaterThan(5);
    expect(inDom).toBeLessThan(60); // 虚拟化：60 行不全量进 DOM

    // 滚到底部：末行进入可视窗口
    const scroller = view.container.querySelector('[data-node-id="col-big-0"]')!.closest('[data-virtual-scroller]') as HTMLElement;
    expect(scroller.querySelector('[data-virtual-spacer]')).toHaveStyle({ height: '1440px' });
    scroller.scrollTop = 60 * 24;
    fireEvent.scroll(scroller);
    await waitFor(() => expect(view.container.querySelector('[data-node-id="col-big-59"]')).not.toBeNull());
    const tail = view.container.querySelector('[data-node-id="col-big-59"]') as HTMLElement;
    expect(tail.parentElement).toHaveStyle({ top: '1416px', height: '24px' });
    fireEvent.mouseDown(tail);
    fireEvent.click(tail);
    const tree = view.container.querySelector('div[tabindex="0"]') as HTMLElement;
    for (let i = 0; i < 59; i++) fireEvent.keyDown(tree, { key: 'ArrowUp' });
    expect(scroller.scrollTop).toBe(0);
    fireEvent.scroll(scroller);
    expect(view.container.querySelector('[data-node-id="col-big-0"]')).toHaveClass('tree-node-focused');
    for (let i = 0; i < 59; i++) fireEvent.keyDown(tree, { key: 'ArrowDown' });
    expect(scroller.scrollTop).toBe(60 * 24 - 600);
    fireEvent.scroll(scroller);
    expect(view.container.querySelector('[data-node-id="col-big-59"]')).toHaveClass('tree-node-selected');
  });
});

describe('CollectionTree more menu', () => {
  it.each(['collection', 'folder', 'request'] as const)('preserves %s sibling drag sorting at compact row heights', async (kind) => {
    const parentId = kind === 'collection' ? '' : 'c1';
    const siblings = ['a', 'b', 'c'].map((id, sortOrder) => ({ ...nodes[0], id, name: id, kind, parentId, sortOrder }));
    ipc.listNodes.mockResolvedValue(kind === 'collection' ? siblings : [nodes[0], ...siblings]);
    const view = renderTree();
    await screen.findByText('a');
    const source = view.container.querySelector('[data-node-id="a"]') as HTMLElement;
    const target = view.container.querySelector('[data-node-id="c"]') as HTMLElement;
    vi.spyOn(target, 'getBoundingClientRect').mockReturnValue({ top: 48, bottom: 72, height: 24 } as DOMRect);
    fireEvent.mouseDown(source);
    fireEvent.click(source);
    const dataTransfer = { effectAllowed: '', setData: vi.fn() };
    fireEvent.dragStart(source, { dataTransfer });
    fireEvent.dragOver(target, { dataTransfer, clientY: 70 });
    fireEvent.drop(target, { dataTransfer });
    await waitFor(() => expect(ipc.moveNodes).toHaveBeenCalledWith('ws1', [
      { id: 'b', parentId, sortOrder: 0 },
      { id: 'c', parentId, sortOrder: 1 },
      { id: 'a', parentId, sortOrder: 2 },
    ]));
  });

  async function openCollectionMenu() {
    const row = (await screen.findByText('集合1')).closest('[data-node-id]') as HTMLElement;
    fireEvent.click(within(row).getByRole('button', { name: /操作/ }));
    return screen.getByRole('menu');
  }

  it('portals the context menu to body outside the clipping sidebar and closes on outside click', async () => {
    const view = renderTree();
    const menu = await openCollectionMenu();
    expect(menu.parentElement).toBe(document.body);
    expect(view.container).not.toContainElement(menu);
    expect(menu).toHaveClass('fixed', 'overflow-y-auto');
    fireEvent.click(document.body);
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
  });

  it('matches right-click actions without changing multi-selection or expansion', async () => {
    const view = renderTree();
    await screen.findByText('请求1');
    for (const id of ['r1', 'r2']) {
      const row = view.container.querySelector('[data-node-id="' + id + '"]') as HTMLElement;
      fireEvent.mouseDown(row, { ctrlKey: true });
      fireEvent.click(row, { ctrlKey: true });
    }
    const collection = view.container.querySelector('[data-node-id="c1"]') as HTMLElement;
    const more = within(collection).getByRole('button', { name: /操作/ });
    await userEvent.click(more);
    expect(screen.getByText('已选 2 项')).toBeInTheDocument();
    expect(screen.getByText('请求1')).toBeInTheDocument();
    await userEvent.keyboard('{Escape}');
    expect(more).toHaveFocus();
    await userEvent.keyboard('{Enter}');
    const menu = screen.getByRole('menu');
    expect(screen.getByText('已选 2 项')).toBeInTheDocument();
    expect(screen.getByText('请求1')).toBeInTheDocument();
    const labels = within(menu).getAllByRole('button').map((button) => button.textContent);
    expect(labels).toEqual(['重命名', '添加请求', '添加文件夹', 'Runner 批量运行', 'Mock Server', '导出集合', '导出为 Git 友好目录镜像', '删除']);
    expect(menu).toHaveClass('overflow-y-auto');
    const top = parseFloat(menu.style.top);
    expect(top + parseFloat(menu.style.maxHeight)).toBeLessThanOrEqual(window.innerHeight - 8);
    await userEvent.keyboard('{Escape}');
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
    expect(more).toHaveFocus();
    fireEvent.contextMenu(collection, { clientX: window.innerWidth, clientY: window.innerHeight });
    const context = screen.getByRole('menu');
    expect(within(context).getAllByRole('button').map((button) => button.textContent)).toEqual(labels);
    expect(parseFloat(context.style.top) + parseFloat(context.style.maxHeight)).toBe(window.innerHeight - 8);
  });

  it('opens add, runner, mock, rename and delete actions for the target collection', async () => {
    renderTree();
    await openCollectionMenu();
    fireEvent.click(screen.getByRole('button', { name: '添加请求' }));
    await waitFor(() => expect(ipc.upsertNode).toHaveBeenCalledWith(expect.objectContaining({ parentId: 'c1', kind: 'request' })));
    await openCollectionMenu();
    fireEvent.click(screen.getByRole('button', { name: '重命名' }));
    fireEvent.change(within(await screen.findByRole('dialog')).getByRole('textbox'), { target: { value: '改名集合' } });
    fireEvent.click(screen.getByRole('button', { name: '确定' }));
    await waitFor(() => expect(ipc.renameNode).toHaveBeenCalledWith('ws1', 'c1', '改名集合'));
    await openCollectionMenu();
    fireEvent.click(screen.getByRole('button', { name: 'Runner 批量运行' }));
    expect(screen.getByRole('dialog')).toHaveTextContent('Runner c1');
    await openCollectionMenu();
    fireEvent.click(screen.getByRole('button', { name: 'Mock Server' }));
    expect(screen.getAllByRole('dialog').some((el) => el.textContent === 'Mock c1')).toBe(true);
    await openCollectionMenu();
    fireEvent.click(screen.getByRole('button', { name: '删除' }));
    fireEvent.click(await screen.findByRole('button', { name: '确定' }));
    await waitFor(() => expect(ipc.deleteNode).toHaveBeenCalledWith('ws1', 'c1'));
  });

  it('keeps all export formats and mirror export reachable', async () => {
    renderTree();
    const formats = [['postman', 'Postman v2.1'], ['insomnia', 'Insomnia v4'], ['openapi', 'OpenAPI 3.0.3'], ['openapi3.1', 'OpenAPI 3.1.0'], ['swagger2', 'Swagger 2.0'], ['curl', 'cURL'], ['restclient', 'REST Client (.http)'], ['har', 'HAR 1.2']];
    for (const [format, label] of formats) {
      await openCollectionMenu();
      fireEvent.click(screen.getByRole('button', { name: '导出集合' }));
      expect(screen.getByRole('menu')).toBeInTheDocument();
      fireEvent.click(screen.getByRole('button', { name: label }));
      await waitFor(() => expect(ipc.exportData).toHaveBeenLastCalledWith('c1', format));
      expect(navigator.clipboard.writeText).toHaveBeenCalledWith('exported data');
      fireEvent.click(await screen.findByRole('button', { name: '确定' }));
    }
    ipc.openNativeDirectory.mockResolvedValueOnce('E:/mirror');
    await openCollectionMenu();
    fireEvent.click(screen.getByRole('button', { name: '导出为 Git 友好目录镜像' }));
    await waitFor(() => expect(ipc.exportMirror).toHaveBeenCalledWith('c1', 'E:/mirror'));
    expect(ipc.openNativeDirectory).toHaveBeenCalledWith('选择镜像导出目录');
  });

  it('keeps request disable, rename and delete in its more menu', async () => {
    renderTree();
    const row = (await screen.findByText('请求1')).closest('[data-node-id]') as HTMLElement;
    const more = within(row).getByRole('button', { name: /操作/ });
    ipc.getNode.mockResolvedValueOnce({ ...nodes[1], request: {} } as never);
    fireEvent.click(more);
    expect(within(screen.getByRole('menu')).getAllByRole('button').map((button) => button.textContent)).toEqual(['重命名', '禁用请求', '删除']);
    fireEvent.click(screen.getByRole('button', { name: '禁用请求' }));
    await waitFor(() => expect(ipc.upsertNode).toHaveBeenCalledWith(expect.objectContaining({ id: 'r1', disabled: true })));
    fireEvent.click(more);
    fireEvent.click(screen.getByRole('button', { name: '删除' }));
    fireEvent.click(await screen.findByRole('button', { name: '确定' }));
    await waitFor(() => expect(ipc.deleteNode).toHaveBeenCalledWith('ws1', 'r1'));
  });
});
