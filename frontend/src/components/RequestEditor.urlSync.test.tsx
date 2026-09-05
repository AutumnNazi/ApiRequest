import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
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

function makeTab(id: string, url: string, params: Array<{ key: string; value: string; enabled: boolean }>): Tab {
  const draft = newDefaultRequest();
  draft.url = url;
  draft.params = params;
  return {
    id, workspaceId: 'ws-sync', name: 'request', draft,
    revision: 0, dirty: false, sending: false,
  };
}

// 与 App.tsx 相同的数据流：tab 来自 store，patchDraft 的更新回流到组件
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
        <RequestEditor tab={tab} workspaceId="ws-sync" onSend={vi.fn()} onCancel={vi.fn()} onSave={vi.fn()} />
      </DialogProvider>
    </QueryClientProvider>
  );
}

const clientRef = { current: new QueryClient({ defaultOptions: { queries: { retry: false } } }) };

function renderEditor(tab: Tab) {
  useTabs.setState((s) => ({
    sessions: {
      ...s.sessions,
      'ws-sync': { tabs: [tab], activeId: tab.id },
    },
  }));
  return render(<StatefulEditor tabId={tab.id} />);
}

const urlInput = () => screen.getByPlaceholderText('https://api.example.com/path') as HTMLInputElement;
const keyInputs = (c: ReturnType<typeof render>) =>
  c.getAllByPlaceholderText('Key').slice(0, -1) as HTMLInputElement[]; // 去掉 ghost 行
const valueInputs = (c: ReturnType<typeof render>) =>
  c.getAllByPlaceholderText('Value').slice(0, -1) as HTMLInputElement[];

describe('RequestEditor URL ↔ Params bidirectional sync', () => {
  beforeEach(() => {
    localStorage.clear();
    useTabs.setState({ sessions: {} });
  });

  it('parses URL query into table rows on blur', () => {
    const tab = makeTab('t1', 'https://x.test/p', []);
    const view = renderEditor(tab);
    fireEvent.change(urlInput(), { target: { value: 'https://x.test/p?a=1&b=hello%20world' } });
    fireEvent.blur(urlInput());
    const keys = keyInputs(view);
    const values = valueInputs(view);
    expect(keys.map((k) => k.value)).toEqual(['a', 'b']);
    expect(values.map((v) => v.value)).toEqual(['1', 'hello world']);
  });

  it('does not touch the table while only the base is being edited', () => {
    const tab = makeTab('t1', 'https://x.test/p?a=1', [{ key: 'a', value: '1', enabled: true }]);
    const view = renderEditor(tab);
    fireEvent.change(urlInput(), { target: { value: 'https://y.test/other?a=1' } });
    fireEvent.blur(urlInput());
    expect(keyInputs(view).map((k) => k.value)).toEqual(['a']);
  });

  it('serializes table edits back into the URL immediately, encoding structural characters', () => {
    const tab = makeTab('t1', 'https://x.test/p', []);
    const view = renderEditor(tab);
    const ghostKey = view.getAllByPlaceholderText('Key').at(-1) as HTMLInputElement;
    fireEvent.change(ghostKey, { target: { value: 'q' } });
    // 敲入 key 后 ghost 行转正并生成新 ghost 行，value 输入框需按当前 DOM 重新查询
    fireEvent.change(valueInputs(view)[0], { target: { value: 'a b+c&d' } });
    expect(urlInput().value).toBe('https://x.test/p?q=a%20b%2Bc%26d');
  });

  it('keeps {{var}} templates raw through table→URL serialization', () => {
    const tab = makeTab('t1', 'https://x.test/p', []);
    const view = renderEditor(tab);
    const ghostKey = view.getAllByPlaceholderText('Key').at(-1) as HTMLInputElement;
    fireEvent.change(ghostKey, { target: { value: 'q' } });
    fireEvent.change(valueInputs(view)[0], { target: { value: '{{$guid}}' } });
    expect(urlInput().value).toBe('https://x.test/p?q={{$guid}}');
  });

  it('does not auto-serialize the table into the URL on mount', () => {
    const tab = makeTab('t1', 'https://x.test/p', [
      { key: 'a', value: '1', enabled: true },
      { key: 'b', value: '2', enabled: false },
    ]);
    renderEditor(tab);
    expect(urlInput().value).toBe('https://x.test/p');
  });

  it('preserves disabled rows when the user edits URL query text', () => {
    const tab = makeTab('t1', 'https://x.test/p?a=1', [
      { key: 'a', value: '1', enabled: true },
      { key: 'z', value: '9', enabled: false },
    ]);
    const view = renderEditor(tab);
    fireEvent.change(urlInput(), { target: { value: 'https://x.test/p?a=1&c=3' } });
    fireEvent.blur(urlInput());
    expect(keyInputs(view).map((k) => k.value)).toEqual(['a', 'c', 'z']);
  });

  it('strips the disabled row from the URL so unchecking really omits the param', () => {
    // 后端 AddParams 里 URL query 优先于表格：若 URL 保留 ?a=1，取消勾选不会
    // 改变实际发送的请求。因此禁用必须实时从 URL 剥除。
    const tab = makeTab('t1', 'https://x.test/p?a=1', [{ key: 'a', value: '1', enabled: true }]);
    const view = renderEditor(tab);
    fireEvent.click(view.getAllByRole('checkbox')[0]);
    expect(urlInput().value).toBe('https://x.test/p');
    // 重新勾选则恢复 query
    fireEvent.click(view.getAllByRole('checkbox')[0]);
    expect(urlInput().value).toBe('https://x.test/p?a=1');
  });

  it('resets the sync baseline when switching to another tab', async () => {
    const tabA = makeTab('tA', 'https://a.test/p?x=1', [{ key: 'x', value: '1', enabled: true }]);
    const tabB = makeTab('tB', 'https://b.test/p', []);
    const view = renderEditor(tabA);
    useTabs.setState((s) => ({
      sessions: {
        ...s.sessions,
        'ws-sync': { tabs: [tabA, tabB], activeId: 'tB' },
      },
    }));
    // StatefulEditor 按 tabId 订阅，切 prop 即换标签（不重挂载）
    view.rerender(<StatefulEditor tabId="tB" />);
    // 新标签无编辑动作：blur 不应清掉它自己的（空）表格，也不产生行
    fireEvent.blur(urlInput());
    expect(keyInputs(view)).toHaveLength(0);
    expect(urlInput().value).toBe('https://b.test/p');
  });

  it('syncs once on Enter send and does not re-parse after Params edits', () => {
    const tab = makeTab('t1', 'https://x.test/p', []);
    const view = renderEditor(tab);
    fireEvent.change(urlInput(), { target: { value: 'https://x.test/p?a=1&b=2' } });
    fireEvent.keyDown(urlInput(), { key: 'Enter' });
    expect(keyInputs(view).map((k) => k.value)).toEqual(['a', 'b']);
    // 表格里改值 → URL 更新；此时再 blur 不应把表格重置回旧 query
    fireEvent.change(valueInputs(view)[0], { target: { value: '9' } });
    fireEvent.blur(urlInput());
    expect(valueInputs(view)[0].value).toBe('9');
    expect(urlInput().value).toBe('https://x.test/p?a=9&b=2');
  });
});

describe('KVTable batch edit mode', () => {
  beforeEach(() => {
    localStorage.clear();
    useTabs.setState({ sessions: {} });
  });

  it('round-trips rows through the batch textarea and preserves descriptions', async () => {
    const draft = newDefaultRequest();
    draft.url = 'https://x.test/p';
    draft.params = [
      { key: 'a', value: '1', enabled: true, description: 'kept' },
      { key: 'b', value: '2', enabled: false },
    ];
    const tab: Tab = {
      id: 't1', workspaceId: 'ws-sync', name: 'request', draft,
      revision: 0, dirty: false, sending: false,
    };
    const view = renderEditor(tab);

    fireEvent.click(screen.getByText('批量编辑'));
    const area = view.getByPlaceholderText(/每行一条/) as HTMLTextAreaElement;
    expect(area.value).toBe('a:1\n//b:2');

    fireEvent.change(area, { target: { value: 'a:9\nb:2' } }); // b 被重新启用，a 值改掉
    fireEvent.click(screen.getByText('应用'));

    await waitFor(() => {
      expect(urlInput().value).toBe('https://x.test/p?a=9&b=2');
    });
    // description 按 key+value 匹配保留：a 的 'kept' 丢了（值变了），但行数据正确
    const keys = keyInputs(view).map((k) => k.value);
    expect(keys).toEqual(['a', 'b']);
    const state = useTabs.getState();
    const saved = Object.values(state.sessions)[0].tabs[0].draft.params;
    expect(saved.map((p) => `${p.key}=${p.value}:${p.enabled}`)).toEqual(['a=9:true', 'b=2:true']);
  });

  it('canceling the batch mode leaves the table untouched', () => {
    const tab = makeTab('t1', 'https://x.test/p?a=1', [{ key: 'a', value: '1', enabled: true }]);
    const view = renderEditor(tab);
    fireEvent.click(screen.getByText('批量编辑'));
    const area = view.getByPlaceholderText(/每行一条/) as HTMLTextAreaElement;
    fireEvent.change(area, { target: { value: 'totally:different' } });
    fireEvent.click(screen.getByText('取消'));
    expect(keyInputs(view).map((k) => k.value)).toEqual(['a']);
    expect(urlInput().value).toBe('https://x.test/p?a=1');
  });

  it('offers batch editing on the Headers pane without touching the URL', () => {
    const tab = makeTab('t1', 'https://x.test/p?a=1', [{ key: 'a', value: '1', enabled: true }]);
    tab.draft.headers = [{ key: 'Accept', value: 'application/json', enabled: true }];
    const view = renderEditor(tab);
    fireEvent.click(screen.getByRole('button', { name: /Headers/ }));
    fireEvent.click(screen.getByText('批量编辑'));
    const area = view.getByPlaceholderText(/每行一条/) as HTMLTextAreaElement;
    fireEvent.change(area, { target: { value: 'Accept:application/json\n//X-Debug:1' } });
    fireEvent.click(screen.getByText('应用'));
    expect(keyInputs(view).map((k) => k.value)).toEqual(['Accept', 'X-Debug']);
    const saved = Object.values(useTabs.getState().sessions)[0].tabs[0].draft;
    expect(saved.headers?.map((h) => `${h.key}=${h.enabled}`)).toEqual([
      'Accept=true',
      'X-Debug=false',
    ]);
    expect(urlInput().value).toBe('https://x.test/p?a=1'); // Headers 不参与 URL 序列化
  });
});

describe('定时器清理', () => {
  // 回归背景：CI 上 jsdom 环境在文件结束后销毁（window 被移除），此前 onBlur 里
  // 裸调度的 setTimeout 仍在 pending，回调 setState 时读 window.event 直接崩溃
  // （本地因时序侥幸不触发）。用间谍跟踪两个 150ms 失焦定时器的调度与清理，
  // 断言卸载后全部清干净（不数 getTimerCount，避免混入 react-query gc 等无关定时器）。
  it('卸载时清理失焦延迟定时器，不逃逸出组件生命周期', () => {
    const origSet = globalThis.setTimeout.bind(globalThis);
    const origClear = globalThis.clearTimeout.bind(globalThis);
    const scheduled = new Set<ReturnType<typeof setTimeout>>();
    const setSpy = vi.spyOn(globalThis, 'setTimeout').mockImplementation(((fn: () => void, delay?: number) => {
      const id = origSet(fn, delay);
      if (delay === 150) scheduled.add(id);
      return id;
    }) as typeof globalThis.setTimeout);
    const clearSpy = vi.spyOn(globalThis, 'clearTimeout').mockImplementation(((id?: ReturnType<typeof setTimeout>) => {
      if (id != null) scheduled.delete(id);
      return origClear(id);
    }) as typeof globalThis.clearTimeout);
    try {
      const view = renderEditor(makeTab('tab-timers', 'https://api.example.com/x?a=1', [
        { key: 'a', value: '1', enabled: true },
      ]));
      fireEvent.blur(urlInput()); // URL 建议框延迟关闭
      fireEvent.blur(view.getAllByPlaceholderText('Key')[0] as HTMLInputElement); // KVTable 自动聚焦提示延迟关闭
      expect(scheduled.size).toBe(2); // 两个失焦延迟定时器已调度
      view.unmount();
      expect(scheduled.size).toBe(0); // 卸载时必须全部清理
    } finally {
      setSpy.mockRestore();
      clearSpy.mockRestore();
      for (const id of scheduled) origClear(id);
    }
  });
});
