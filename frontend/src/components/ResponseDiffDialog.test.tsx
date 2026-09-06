import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, expect, it, vi, beforeEach } from 'vitest';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import ResponseDiffDialog, { type DiffSide } from './ResponseDiffDialog';

const ipc = vi.hoisted(() => ({
  listHistory: vi.fn(),
  getHistory: vi.fn(),
  readResponseBlobRange: vi.fn(),
  listExamples: vi.fn(),
  toAppError: vi.fn((e: unknown) => ({ kind: 'unknown', detail: String(e) })),
}));

vi.mock('../ipc', () => ipc);

const base: DiffSide = {
  label: 'https://x.test/api',
  status: 200,
  durationMs: 100,
  sizeBytes: 10,
  bodyText: '{"a":2}',
};

function renderDialog() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <ResponseDiffDialog workspaceId="ws-1" base={base} onClose={vi.fn()} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  ipc.listHistory.mockReset();
  ipc.getHistory.mockReset();
  ipc.listHistory.mockResolvedValue({
    items: [
      { id: 'h1', method: 'GET', url: 'https://x.test/api', status: 201, durationMs: 50, sizeBytes: 8, hasBody: true, createdAt: 2, workspaceId: 'ws-1' },
      { id: 'h2', method: 'POST', url: 'https://x.test/other', status: 500, durationMs: 10, sizeBytes: 4, hasBody: false, createdAt: 1, workspaceId: 'ws-1' },
    ],
    hasMore: false,
    nextCursor: '',
  });
});

describe('ResponseDiffDialog', () => {
  it('lists history entries and shows a normalized JSON diff after picking one', async () => {
    ipc.getHistory.mockResolvedValue({
      id: 'h1', workspaceId: 'ws-1', requestSnap: {}, status: 201, durationMs: 50,
      sizeBytes: 8, respHeaders: [], bodyInline: '{"a":1}', createdAt: 2,
    });
    const { container } = renderDialog();

    // 历史列表出现（搜索框预填当前 URL）
    expect(await screen.findByText('https://x.test/api')).toBeInTheDocument();
    expect(screen.getByPlaceholderText(/搜索 URL/)).toHaveValue('https://x.test/api');

    fireEvent.click(screen.getByText('https://x.test/api'));

    // 归一化 diff：两侧 JSON 展开后 "a" 的值不同
    await waitFor(() => {
      expect(container.querySelector('[data-diff="del"]')).not.toBeNull();
    });
    expect(container.querySelector('[data-diff="del"]')?.textContent).toContain('"a": 2');
    expect(container.querySelector('[data-diff="add"]')?.textContent).toContain('"a": 1');
    // 两侧状态码汇总
    expect(container.querySelector('[data-side="other-status"]')?.textContent).toBe('201');
    expect(container.querySelector('[data-side="base-status"]')?.textContent).toBe('200');
  });

  it('falls back to raw lines when normalization is disabled', async () => {
    ipc.getHistory.mockResolvedValue({
      id: 'h1', workspaceId: 'ws-1', requestSnap: {}, status: 201, durationMs: 50,
      sizeBytes: 8, respHeaders: [], bodyInline: '{"a":1}', createdAt: 2,
    });
    const { container } = renderDialog();
    fireEvent.click(await screen.findByText('https://x.test/api'));
    await waitFor(() => {
      expect(container.querySelector('[data-diff="del"]')).not.toBeNull();
    });
    fireEvent.click(screen.getByRole('checkbox'));
    await waitFor(() => {
      expect(container.querySelector('[data-diff="del"]')?.textContent).toBe('- {"a":2}');
    });
    expect(container.querySelector('[data-diff="add"]')?.textContent).toBe('+ {"a":1}');
  });

  it('shows an empty state when no history matches', async () => {
    ipc.listHistory.mockResolvedValue({ items: [], hasMore: false, nextCursor: '' });
    renderDialog();
    expect(await screen.findByText(/没有匹配的历史记录/)).toBeInTheDocument();
  });
});

describe('ResponseDiffDialog 示例对照', () => {
  const exampleSide = {
    id: 'ex1', nodeId: 'node-1', name: '预期 200', status: 201, headers: [], body: '{"a":1,"b":2}',
    createdAt: 1, updatedAt: 1,
  };

  it('switches to the examples source and diffs against a saved example', async () => {
    ipc.listExamples.mockResolvedValue([exampleSide]);
    const { container } = render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <ResponseDiffDialog workspaceId="ws-1" nodeId="node-1" base={base} onClose={vi.fn()} />
      </QueryClientProvider>,
    );

    fireEvent.click(screen.getByRole('button', { name: '示例' }));
    fireEvent.click(await screen.findByText('预期 200'));

    await waitFor(() => {
      expect(container.querySelector('[data-diff="del"]')).not.toBeNull();
    });
    const addRows = [...container.querySelectorAll('[data-diff="add"]')].map((el) => el.textContent ?? '');
    expect(addRows.some((t) => t.includes('"b": 2'))).toBe(true);
  });
});
