import { describe, expect, it, vi, beforeEach } from 'vitest';
import { normalizeImportedCookies } from './CookieManager';

describe('normalizeImportedCookies', () => {
  it('normalizes legacy fields and preserves domain scope', () => {
    const [cookie] = normalizeImportedCookies([{
      name: 'session', value: 'secret', domain: '.example.test', sameSite: 'Lax', secure: true,
    }]);
    expect(cookie).toMatchObject({
      name: 'session', value: 'secret', domain: '.example.test', path: '/',
      sameSite: 'lax', secure: true, hostOnly: false, expires: 0,
    });
  });

  it('rejects an invalid SameSite value before starting the batch', () => {
    expect(() => normalizeImportedCookies([{
      name: 'session', domain: 'example.test', sameSite: 'sometimes',
    }])).toThrow('SameSite');
  });
});

// ── 工作区级 Cookie Jar：所有读写必须携带 workspaceId ──
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import CookieManager from './CookieManager';
import { DialogProvider } from './DialogProvider';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

const ipc = vi.hoisted(() => ({
  listCookies: vi.fn((_workspaceId: string, _domain?: string) => Promise.resolve([] as import('../ipc').Cookie[])),
  clearCookies: vi.fn(() => Promise.resolve()),
  deleteCookie: vi.fn(() => Promise.resolve()),
  upsertCookie: vi.fn(() => Promise.resolve()),
  upsertCookies: vi.fn(() => Promise.resolve()),
  openNativeFile: vi.fn(() => Promise.resolve('')),
  readNativeTextFile: vi.fn(() => Promise.resolve('')),
}));

vi.mock('../ipc', () => ({
  ...ipc,
  toAppError: (cause: unknown) => ({ detail: cause instanceof Error ? cause.message : String(cause) }),
}));

describe('workspace-scoped cookie jar', () => {
  it('lists and clears cookies for the current workspace only', async () => {
    ipc.listCookies.mockResolvedValue([
      { name: 'session', value: 'v', domain: 'a.test', path: '/', httpOnly: false, secure: false, hostOnly: true },
    ]);
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <DialogProvider><CookieManager workspaceId="ws-1" onClose={() => undefined} /></DialogProvider>
      </QueryClientProvider>,
    );
    await screen.findByText('a.test');
    expect(ipc.listCookies).toHaveBeenCalledWith('ws-1');
    // 清空走确认弹窗
    fireEvent.click(screen.getByRole('button', { name: '全部清空' }));
    const confirm = await screen.findByRole('button', { name: '确定' });
    fireEvent.click(confirm);
    await waitFor(() => expect(ipc.clearCookies).toHaveBeenCalledWith('ws-1'));
  });
});
