import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, expect, it, vi, beforeEach } from 'vitest';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import CommandPalette from './CommandPalette';
import { useTabs } from '../stores/tabs';

const ipc = vi.hoisted(() => ({
  listNodes: vi.fn(),
  getNode: vi.fn(),
  listWorkspaces: vi.fn(),
  listEnvironments: vi.fn(),
  setActiveEnvironment: vi.fn(),
  newDefaultRequest: vi.fn(() => ({ method: 'GET', url: '' })),
  toAppError: vi.fn((e: unknown) => ({ kind: 'unknown', detail: String(e) })),
}));

vi.mock('../ipc', () => ipc);

function renderPalette(props: Partial<Parameters<typeof CommandPalette>[0]> = {}) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <CommandPalette
        workspaceId="ws-1"
        onSwitchWorkspace={vi.fn()}
        onClose={vi.fn()}
        {...props}
      />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  ipc.listNodes.mockResolvedValue([
    { id: 'n1', workspaceId: 'ws-1', parentId: 'c1', kind: 'request', name: 'login user', sortOrder: 1, createdAt: 1 },
    { id: 'n2', workspaceId: 'ws-1', parentId: 'c1', kind: 'request', name: 'list orders', sortOrder: 2, createdAt: 2 },
    { id: 'c1', workspaceId: 'ws-1', parentId: '', kind: 'collection', name: '核心集合', sortOrder: 0, createdAt: 3 },
  ] as never);
  ipc.getNode.mockResolvedValue({
    id: 'n1', workspaceId: 'ws-1', kind: 'request', name: 'login user',
    request: { method: 'GET', url: 'https://x.test' },
  });
  ipc.listWorkspaces.mockResolvedValue([
    { id: 'ws-1', name: '默认' },
    { id: 'ws-2', name: '工作区二号' },
  ]);
  ipc.listEnvironments.mockResolvedValue([
    { id: 'env-1', workspaceId: 'ws-1', name: 'staging', variables: [], isActive: false, createdAt: 1, updatedAt: 1 },
  ]);
  useTabs.setState({ sessions: {} });
});

describe('CommandPalette', () => {
  it('filters commands by keyword and jumps to a request via openNode', async () => {
    renderPalette();

    const input = screen.getByPlaceholderText(/输入以筛选/);
    fireEvent.change(input, { target: { value: 'login' } });

    // 请求命中、其余被过滤（"工作区二号"不应出现）
    const item = await screen.findByText('login user');
    expect(item).toBeInTheDocument();
    expect(screen.queryByText('工作区二号')).not.toBeInTheDocument();

    fireEvent.click(item);
    await waitFor(() => expect(ipc.getNode).toHaveBeenCalledWith('ws-1', 'n1'));
    const session = useTabs.getState().sessions['ws-1'];
    expect(session?.tabs.length).toBe(1);
    expect(session?.tabs[0].nodeId).toBe('n1');
  });

  it('switches environment via keyboard selection', async () => {
    renderPalette();

    fireEvent.change(screen.getByPlaceholderText(/输入以筛选/), { target: { value: 'staging' } });
    await screen.findByText('staging');
    fireEvent.keyDown(screen.getByPlaceholderText(/输入以筛选/), { key: 'Enter' });

    await waitFor(() => expect(ipc.setActiveEnvironment).toHaveBeenCalledWith('ws-1', 'env-1'));
  });

  it('switches workspace', async () => {
    const onSwitchWorkspace = vi.fn();
    renderPalette({ onSwitchWorkspace });

    fireEvent.change(screen.getByPlaceholderText(/输入以筛选/), { target: { value: '二号' } });
    const item = await screen.findByText('工作区二号');
    fireEvent.click(item);
    expect(onSwitchWorkspace).toHaveBeenCalledWith('ws-2', '工作区二号');
  });

  it('keyboard arrows move the active row and Enter executes', async () => {
    renderPalette();
    const input = screen.getByPlaceholderText(/输入以筛选/);

    // 无关键字：全部命令在列。回车执行高亮行（第 5 行 = 新建请求动作）
    await screen.findByText('login user');
    await screen.findByText('工作区二号');
    await screen.findByText('staging');
    fireEvent.keyDown(input, { key: 'ArrowDown' });
    fireEvent.keyDown(input, { key: 'ArrowDown' });
    fireEvent.keyDown(input, { key: 'ArrowDown' });
    fireEvent.keyDown(input, { key: 'ArrowDown' });
    fireEvent.keyDown(input, { key: 'Enter' });

    // 向下 4 步后落在"新建请求"动作上：store 出现无 nodeId 的空白标签
    await waitFor(() => {
      const session = useTabs.getState().sessions['ws-1'];
      expect(session?.tabs.length).toBe(1);
    });
    const session = useTabs.getState().sessions['ws-1'];
    expect(session?.tabs[0].nodeId).toBeUndefined();
  });
});
