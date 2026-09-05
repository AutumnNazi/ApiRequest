// GraphqlPanel 的 Query 校验框：内省后可用 schemaJson 校验粘贴的 query
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi, beforeEach } from 'vitest';
import GraphqlPanel from './GraphqlPanel';

const schemaJson = JSON.stringify({
  queryType: { name: 'Query' },
  types: [
    {
      kind: 'OBJECT', name: 'Query',
      fields: [
        { name: 'user', type: { kind: 'OBJECT', name: 'User', ofType: null }, args: [] },
      ],
    },
    {
      kind: 'OBJECT', name: 'User',
      fields: [
        { name: 'name', type: { kind: 'SCALAR', name: 'String', ofType: null }, args: [] },
      ],
    },
  ],
});

const ipc = vi.hoisted(() => ({
  graphqlIntrospect: vi.fn(),
  openRequest: vi.fn(),
}));

vi.mock('../ipc', () => ({
  graphqlIntrospect: ipc.graphqlIntrospect,
  openRequest: ipc.openRequest,
  toAppError: (cause: unknown) => ({ detail: cause instanceof Error ? cause.message : String(cause) }),
}));

beforeEach(() => {
  vi.clearAllMocks();
  ipc.graphqlIntrospect.mockResolvedValue({
    schemaJson,
    queries: [{ name: 'user', returnType: 'User', returnKind: 'OBJECT', args: '[]' }],
    mutations: [],
  });
});

describe('GraphqlPanel query validation', () => {
  it('reports schema issues for an invalid pasted query', async () => {
    render(<GraphqlPanel onClose={vi.fn()} />);
    const urlInput = screen.getByPlaceholderText(/https:\/\/api\.example\.com\/graphql/) as HTMLInputElement;
    fireEvent.change(urlInput, { target: { value: 'https://api.test/graphql' } });
    fireEvent.click(screen.getByRole('button', { name: '内省 Schema' }));

    const area = await screen.findByPlaceholderText(/query \{ user/) as HTMLTextAreaElement;
    fireEvent.change(area, { target: { value: 'query { user { nope } }' } });
    fireEvent.click(screen.getByRole('button', { name: '校验' }));

    await waitFor(() => expect(screen.getByText(/1 个问题/)).toBeInTheDocument());
    // 错误消息（含类型与行号）与 textarea 内容都可能含 nope，取错误行
    const issueLines = screen.getAllByText(/nope/);
    expect(issueLines.length).toBeGreaterThan(0);
  });

  it('shows validation passed for a valid query', async () => {
    render(<GraphqlPanel onClose={vi.fn()} />);
    const urlInput = screen.getByPlaceholderText(/https:\/\/api\.example\.com\/graphql/) as HTMLInputElement;
    fireEvent.change(urlInput, { target: { value: 'https://api.test/graphql' } });
    fireEvent.click(screen.getByRole('button', { name: '内省 Schema' }));

    const area = await screen.findByPlaceholderText(/query \{ user/) as HTMLTextAreaElement;
    fireEvent.change(area, { target: { value: 'query { user { name } }' } });
    fireEvent.click(screen.getByRole('button', { name: '校验' }));

    await waitFor(() => expect(screen.getByText('校验通过')).toBeInTheDocument());
  });
});
