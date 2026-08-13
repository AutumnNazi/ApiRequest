import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, expect, it, vi, beforeEach } from 'vitest';
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
  toAppError: vi.fn((e: unknown) => (e instanceof Error ? e : new Error(String(e)))),
}));

vi.mock('../ipc', () => ipc);

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

beforeEach(() => {
  vi.clearAllMocks();
  ipc.listNodes.mockReset();
  ipc.listNodes.mockResolvedValue(nodes);
  ipc.deleteNode.mockResolvedValue(null);
  ipc.moveNodes.mockResolvedValue(null);
  useTabs.setState({ sessions: {} });
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

    const expander = screen.getByText('▾').closest('button') as HTMLElement;
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
    const collection = view.container.querySelector('[data-node-id="c1"] > div') as HTMLElement;
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
});
