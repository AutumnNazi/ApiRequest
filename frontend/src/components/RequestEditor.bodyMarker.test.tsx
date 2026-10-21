// Body 页签的计数标记：从其他页签（Params/Headers/脚本区）也能看出
// 请求带了 body 及其体量——raw/graphql 显示字节大小，表单显示启用条目数
// （对齐 Params/Headers 的 "(n)" 约定，避免与标签页"未保存圆点"混淆）。
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { DialogProvider } from './DialogProvider';
import RequestEditor from './RequestEditor';
import { newDefaultRequest } from '../ipc';
import type { Body } from '../ipc';
import type { Tab } from '../stores/tabs';

vi.mock('../ipc', async (importOriginal) => {
  const original = await importOriginal<typeof import('../ipc')>();
  return {
    ...original,
    listHistory: vi.fn(async () => ({ items: [], hasMore: false, nextCursor: '' })),
  };
});

function makeTab(draft: ReturnType<typeof newDefaultRequest>): Tab {
  return {
    id: 'tab-body', workspaceId: 'workspace-1', name: 'request', draft,
    revision: 0, dirty: false, sending: false,
  };
}

function renderEditor(draft: ReturnType<typeof newDefaultRequest>) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <DialogProvider>
        <RequestEditor
          tab={makeTab(draft)} workspaceId="workspace-1"
          onSend={vi.fn()} onCancel={vi.fn()} onSave={vi.fn()}
        />
      </DialogProvider>
    </QueryClientProvider>,
  );
}

const bodyTab = (): HTMLButtonElement =>
  screen.getByRole('button', { name: /^Body/ }) as HTMLButtonElement;

describe('Body 页签内容标记', () => {
  it('kind=none 或空 raw：无标记', () => {
    renderEditor(newDefaultRequest());
    expect(bodyTab().textContent).toBe('Body');
  });

  it('raw 有文本：Body 页签带 • 标记', () => {
    const draft = newDefaultRequest();
    draft.body = { ...draft.body, kind: 'raw', language: 'json', text: '{"a":1}' } as Body;
    renderEditor(draft);
    expect(bodyTab().textContent).toBe('Body (7 B)');
  });

  it('raw 仅空白字符：视为未填写', () => {
    const draft = newDefaultRequest();
    draft.body = { ...draft.body, kind: 'raw', language: 'json', text: '   \n  ' } as Body;
    renderEditor(draft);
    expect(bodyTab().textContent).toBe('Body');
  });

  it('urlencoded/formdata 有启用条目：带标记', () => {
    const draft = newDefaultRequest();
    draft.body = {
      ...draft.body, kind: 'urlencoded',
      items: [
        { key: 'a', value: '1', type: 'text', enabled: true },
        { key: 'b', value: '2', type: 'text', enabled: false },
      ],
    } as Body;
    renderEditor(draft);
    // 仅 1 条启用（另一条 disabled 被过滤）
    expect(bodyTab().textContent).toBe('Body (1)');
  });

  it('formdata 全部禁用：无标记', () => {
    const draft = newDefaultRequest();
    draft.body = {
      ...draft.body, kind: 'formdata',
      items: [{ key: 'a', value: '1', type: 'text', enabled: false }],
    } as Body;
    renderEditor(draft);
    expect(bodyTab().textContent).toBe('Body');
  });

  it('binary 有路径 / graphql 有 query：带标记', () => {
    const bin = newDefaultRequest();
    bin.body = { ...bin.body, kind: 'binary', path: 'C:/data.bin' } as Body;
    const { unmount } = renderEditor(bin);
    expect(bodyTab().textContent).toBe('Body (1)');
    unmount();

    const gql = newDefaultRequest();
    gql.body = { ...gql.body, kind: 'graphql', query: '{ user { id } }' } as Body;
    renderEditor(gql);
    expect(bodyTab().textContent).toBe('Body (15 B)');
  });
});
