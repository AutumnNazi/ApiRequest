import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { DialogProvider } from './DialogProvider';
import RequestEditor from './RequestEditor';
import { newDefaultRequest } from '../ipc';
import type { Tab } from '../stores/tabs';

vi.mock('../ipc', async (importOriginal) => {
  const original = await importOriginal<typeof import('../ipc')>();
  return {
    ...original,
    listHistory: vi.fn(async () => ({ items: [], hasMore: false, nextCursor: '' })),
  };
});

describe('RequestEditor send keyboard handling', () => {
  it('keeps URL controls and request tabs in their responsive regions', () => {
    const draft = newDefaultRequest();
    draft.url = 'https://example.test/a/long/path';
    const tab: Tab = {
      id: 'tab-narrow', workspaceId: 'workspace-1', name: 'request', draft,
      revision: 0, dirty: true, sending: true,
    };
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const view = render(
      <QueryClientProvider client={client}>
        <DialogProvider>
          <RequestEditor tab={tab} workspaceId="workspace-1" onSend={vi.fn()} onCancel={vi.fn()} onSave={vi.fn()} />
        </DialogProvider>
      </QueryClientProvider>,
    );
    const url = screen.getByPlaceholderText('https://api.example.com/path');
    const toolbar = url.parentElement?.parentElement?.parentElement;
    expect(toolbar).toHaveClass('request-toolbar');
    expect(url.closest('.request-url')).toContainElement(screen.getByRole('button', { name: 'GET' }));
    expect(toolbar).toContainElement(screen.getByRole('button', { name: '取消' }));
    expect(toolbar).toContainElement(screen.getByRole('button', { name: /保存/ }));
    expect(screen.getByRole('button', { name: '设置' }).parentElement).toHaveClass('panel-tabs');
  });

  it('sends once and stops Ctrl/Cmd+Enter from reaching the global hotkey', () => {
    const draft = newDefaultRequest();
    draft.url = 'https://example.test';
    const tab: Tab = {
      id: 'tab-1', workspaceId: 'workspace-1', name: 'request', draft,
      revision: 0, dirty: false, sending: false,
    };
    const onSend = vi.fn();
    const globalKey = vi.fn();
    window.addEventListener('keydown', globalKey);
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={client}>
        <DialogProvider>
          <RequestEditor tab={tab} workspaceId="workspace-1" onSend={onSend} onCancel={vi.fn()} onSave={vi.fn()} />
        </DialogProvider>
      </QueryClientProvider>,
    );

    fireEvent.keyDown(screen.getByPlaceholderText('https://api.example.com/path'), {
      key: 'Enter', ctrlKey: true,
    });

    expect(onSend).toHaveBeenCalledTimes(1);
    expect(globalKey).not.toHaveBeenCalled();
    window.removeEventListener('keydown', globalKey);
  });
});
