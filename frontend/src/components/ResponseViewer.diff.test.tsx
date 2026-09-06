import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ResponseResult } from '../ipc';
import ResponseViewer from './ResponseViewer';

const ipc = vi.hoisted(() => ({
  listHistory: vi.fn(() => Promise.resolve({ items: [], hasMore: false, nextCursor: '' })),
  getHistory: vi.fn(() => Promise.resolve(null)),
  readResponseBlobRange: vi.fn(),
  upsertExample: vi.fn(),
  getResponseBlobInfo: vi.fn(),
  saveResponseBlob: vi.fn(),
  saveNativeFile: vi.fn(),
  toAppError: vi.fn((e: unknown) => ({ kind: 'unknown', detail: String(e) })),
}));

vi.mock('../ipc', () => ipc);

const response = {
  status: 200,
  statusText: 'OK',
  headers: [],
  cookies: [],
  body: { inline: true, text: '{"a":1}', encoding: 'utf8' },
  timing: { dnsMs: 0, connectMs: 0, tlsMs: 0, ttfbMs: 1, downloadMs: 1, totalMs: 2 },
  sizeBytes: 7,
  testResults: [],
  scriptLogs: [],
} as unknown as ResponseResult;

describe('ResponseViewer 对比入口', () => {
  it('opens the diff dialog wired with the current response as base', async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const view = render(
      <QueryClientProvider client={client}>
        <ResponseViewer response={response} sending={false} workspaceId="ws-1" requestUrl="https://x.test/api" />
      </QueryClientProvider>,
    );
    fireEvent.click(screen.getByRole('button', { name: '对比' }));
    expect(await screen.findByText('响应对比')).toBeInTheDocument();
    // 历史列表以当前请求 URL 预填搜索
    expect(screen.getByPlaceholderText(/搜索 URL/)).toHaveValue('https://x.test/api');
    expect(await screen.findByText(/没有匹配的历史记录/)).toBeInTheDocument();
    view.unmount();
  });

  it('hides the compare button without a workspace context', () => {
    render(<ResponseViewer response={response} sending={false} />);
    expect(screen.queryByRole('button', { name: '对比' })).not.toBeInTheDocument();
  });
});
