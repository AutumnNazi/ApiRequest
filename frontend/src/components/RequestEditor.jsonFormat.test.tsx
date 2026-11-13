// Body 的「格式化 / 压缩」按钮：只改空白，不改字面量。
// 回归点：JS 的 Number 是双精度，超过 2^53-1 的整数一旦走 JSON.parse + JSON.stringify
// 就会被静默改写（1547040075470688258 -> 1547040075470688300）。
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { DialogProvider } from './DialogProvider';
import RequestEditor from './RequestEditor';
import { newDefaultRequest, type Body } from '../ipc';
import { useLocale } from '../i18n/locale';
import { useTabs } from '../stores/tabs';
import type { Tab } from '../stores/tabs';

vi.mock('../ipc', async (importOriginal) => {
  const original = await importOriginal<typeof import('../ipc')>();
  return {
    ...original,
    listHistory: vi.fn(async () => ({ items: [], hasMore: false, nextCursor: '' })),
  };
});

const BIG_INT_JSON = '{"id":1547040075470688258}';

function renderRawBodyEditor() {
  const draft = newDefaultRequest();
  draft.body = { ...draft.body, kind: 'raw', language: 'json', text: BIG_INT_JSON } as Body;
  const tab: Tab = {
    id: 'tab-json-format', workspaceId: 'workspace-1', name: 'request', draft,
    revision: 0, dirty: false, sending: false,
  };
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <DialogProvider>
        <RequestEditor tab={tab} workspaceId="workspace-1" onSend={vi.fn()} onCancel={vi.fn()} onSave={vi.fn()} />
      </DialogProvider>
    </QueryClientProvider>,
  );
  fireEvent.click(screen.getByRole('button', { name: /^Body/ }));
}

describe('Body JSON 格式化 / 压缩', () => {
  beforeEach(() => useLocale.getState().setLocale('zh-CN'));
  afterEach(() => vi.restoreAllMocks());

  // 测试用的 tab 不在 store 里，patchDraft 落不到会话表；这里直接截获它收到的 body.text
  it('格式化保留超出 2^53-1 的整数', () => {
    const spy = vi.spyOn(useTabs.getState(), 'patchDraft');
    renderRawBodyEditor();
    fireEvent.click(screen.getByRole('button', { name: '格式化' }));

    const patch = spy.mock.calls.at(-1)?.[1] as { body?: Body } | undefined;
    expect(patch?.body?.text).toBe('{\n  "id": 1547040075470688258\n}');
  });

  it('压缩保留超出 2^53-1 的整数', () => {
    const spy = vi.spyOn(useTabs.getState(), 'patchDraft');
    renderRawBodyEditor();
    fireEvent.click(screen.getByRole('button', { name: '压缩' }));

    const patch = spy.mock.calls.at(-1)?.[1] as { body?: Body } | undefined;
    expect(patch?.body?.text).toBe('{"id":1547040075470688258}');
  });
});
