import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { DialogProvider } from './DialogProvider';
import RequestEditor from './RequestEditor';
import { newDefaultRequest } from '../ipc';
import { useTabs, type Tab } from '../stores/tabs';

vi.mock('../ipc', async (importOriginal) => {
  const original = await importOriginal<typeof import('../ipc')>();
  return {
    ...original,
    listHistory: vi.fn(async () => ({ items: [], hasMore: false, nextCursor: '' })),
  };
});

function makeTab(id: string, method: string): Tab {
  const draft = newDefaultRequest();
  draft.method = method;
  return {
    id, workspaceId: 'ws-method', name: 'request', draft,
    revision: 0, dirty: false, sending: false,
  };
}

const clientRef = { current: new QueryClient({ defaultOptions: { queries: { retry: false } } }) };

function StatefulEditor({ tabId }: { tabId: string }) {
  const tab = useTabs((s) => {
    for (const session of Object.values(s.sessions)) {
      const found = session.tabs.find((t) => t.id === tabId);
      if (found) return found;
    }
    return null;
  });
  if (!tab) return null;
  return (
    <QueryClientProvider client={clientRef.current}>
      <DialogProvider>
        <RequestEditor tab={tab} workspaceId="ws-method" onSend={vi.fn()} onCancel={vi.fn()} onSave={vi.fn()} />
      </DialogProvider>
    </QueryClientProvider>
  );
}

function renderEditor(tab: Tab) {
  useTabs.setState((s) => ({
    sessions: {
      ...s.sessions,
      'ws-method': { tabs: [tab], activeId: tab.id },
    },
  }));
  return render(<StatefulEditor tabId={tab.id} />);
}

const METHODS = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS'];

describe('RequestEditor 请求方式选择器', () => {
  beforeEach(() => {
    localStorage.clear();
    useTabs.setState({ sessions: {} });
  });

  it('是可展开的真下拉：点击即列出全部方法，选择后生效并收起', () => {
    renderEditor(makeTab('t1', 'GET'));
    const trigger = screen.getByRole('button', { name: 'GET' });
    fireEvent.click(trigger);
    // 全部 7 个方法可选项立即可见（回归：datalist 输入框点击无选项）
    for (const m of METHODS) {
      expect(screen.getAllByRole('button', { name: m }).length).toBeGreaterThan(0);
    }
    // 菜单中选 POST（触发按钮显示 GET，菜单里的 POST 唯一）
    fireEvent.click(screen.getByRole('button', { name: 'POST' }));
    // 选择生效：触发按钮变为 POST，且收起菜单
    expect(screen.getByRole('button', { name: 'POST' })).toBeInTheDocument();
    expect(screen.queryAllByRole('button', { name: 'PUT' })).toHaveLength(0);
    // 数据层生效：store 中 draft.method 更新
    const stored = useTabs.getState().sessions['ws-method'].tabs[0];
    expect(stored.draft.method).toBe('POST');
  });

  it('不再是 datalist 文本输入（回归：默认文本框宽度挤占 URL 行）', () => {
    renderEditor(makeTab('t2', 'GET'));
    expect(document.querySelector('input[list="http-methods"]')).toBeNull();
    expect(document.querySelector('#http-methods')).toBeNull();
    // URL 输入仍是行内主输入
    expect(screen.getByPlaceholderText('https://api.example.com/path')).toBeInTheDocument();
  });
});
