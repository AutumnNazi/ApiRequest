import { beforeEach, describe, expect, it, vi } from 'vitest';
import { newDefaultRequest } from '../ipc';
import {
  flushWorkspaceSessions,
  migrateWorkspaceSessions,
  serializeWorkspaceSessions,
  useTabs,
} from './tabs';

describe('Workspace Session store', () => {
  beforeEach(() => {
    vi.useRealTimers();
    localStorage.clear();
    useTabs.setState({ sessions: {} });
    flushWorkspaceSessions();
  });

  it('isolates tabs and active ids by workspace', () => {
    const first = useTabs.getState().openBlank('workspace-a');
    const second = useTabs.getState().openBlank('workspace-b');
    useTabs.getState().patchDraft(first, { url: 'https://a.example' });
    useTabs.getState().patchDraft(second, { url: 'https://b.example' });

    const sessions = useTabs.getState().sessions;
    expect(sessions['workspace-a'].activeId).toBe(first);
    expect(sessions['workspace-b'].activeId).toBe(second);
    expect(sessions['workspace-a'].tabs[0].draft.url).toBe('https://a.example');
    expect(sessions['workspace-b'].tabs[0].draft.url).toBe('https://b.example');

    useTabs.getState().close(first);
    expect(useTabs.getState().sessions['workspace-a'].tabs).toHaveLength(0);
    expect(useTabs.getState().sessions['workspace-b'].tabs).toHaveLength(1);
  });

  it('atomically returns the latest tab state when closing', () => {
    const tabId = useTabs.getState().openBlank('workspace-a');
    useTabs.getState().setSending(tabId, true, 'send-current');
    const response = { status: 200, body: { blobRef: 'blob-latest' } } as never;
    useTabs.getState().setResponse(tabId, 'send-current', response);

    const removed = useTabs.getState().close(tabId);
    expect(removed?.response?.body?.blobRef).toBe('blob-latest');
    expect(useTabs.getState().sessions['workspace-a'].tabs).toHaveLength(0);
  });

  it('refuses to close when the tab revision changed after validation', () => {
    const tabId = useTabs.getState().openBlank('workspace-a');
    const snapshot = useTabs.getState().sessions['workspace-a'].tabs[0];
    useTabs.getState().patchDraft(tabId, { url: 'https://edited-during-close.example' });

    expect(
      useTabs.getState().closeIfUnchanged(tabId, snapshot.revision, snapshot.sendId),
    ).toEqual({ status: 'changed' });
    expect(useTabs.getState().sessions['workspace-a'].tabs).toHaveLength(1);

    const current = useTabs.getState().sessions['workspace-a'].tabs[0];
    const result = useTabs.getState().closeIfUnchanged(tabId, current.revision, current.sendId);
    expect(result.status).toBe('closed');
    expect(useTabs.getState().sessions['workspace-a'].tabs).toHaveLength(0);
  });

  it('persists only dirty drafts and removes credential and transient state', () => {
    const clean = useTabs.getState().openBlank('workspace-a');
    const dirty = useTabs.getState().openBlank('workspace-a');
    const draft = newDefaultRequest();
    draft.auth = { type: 'bearer', params: { token: 'must-not-persist', label: 'public' } };
    useTabs.getState().patchDraft(dirty, draft);
    useTabs.getState().setSending(dirty, true, 'send-1');

    const persisted = serializeWorkspaceSessions(useTabs.getState().sessions);
    expect(persisted['workspace-a'].tabs.map((tab) => tab.id)).toEqual([dirty]);
    expect(persisted['workspace-a'].tabs[0].draft.auth.params?.token).toBe('');
    expect(persisted['workspace-a'].tabs[0].draft.auth.params?.label).toBe('public');
    expect(persisted['workspace-a'].tabs[0].sending).toBe(false);
    expect(persisted['workspace-a'].tabs[0].sendId).toBeUndefined();
    expect(JSON.stringify(persisted)).not.toContain('must-not-persist');
    expect(JSON.stringify(persisted)).not.toContain(clean);
  });

  it('drops every auth parameter for an unknown auth type', () => {
    const tabId = useTabs.getState().openBlank('workspace-a');
    const draft = newDefaultRequest();
    draft.auth = {
      type: 'custom',
      params: { username: 'possibly-sensitive', arbitrary: 'must-not-persist' },
    };
    useTabs.getState().patchDraft(tabId, draft);

    const persisted = serializeWorkspaceSessions(useTabs.getState().sessions);
    expect(persisted['workspace-a'].tabs[0].draft.auth.params).toEqual({
      username: '',
      arbitrary: '',
    });
    expect(JSON.stringify(persisted)).not.toContain('must-not-persist');
    expect(JSON.stringify(persisted)).not.toContain('possibly-sensitive');
  });

  it('removes sensitive header values from persisted drafts', () => {
    const tabId = useTabs.getState().openBlank('workspace-a');
    const draft = newDefaultRequest();
    draft.headers = [
      { key: 'Authorization', value: 'Bearer must-not-persist', enabled: true, description: '' },
      { key: 'X-Custom-Token', value: 'custom-must-not-persist', enabled: true, description: '' },
      { key: 'X-Trace', value: 'public-trace', enabled: true, description: '' },
    ];
    draft.params = [
      { key: 'password', value: 'query-password', enabled: true, description: '' },
      { key: 'page', value: '1', enabled: true, description: '' },
    ];
    Object.assign(draft.body, {
      kind: 'formdata',
      items: [
        { key: 'api_token', type: 'text', value: 'form-token', path: '', enabled: true },
        { key: 'secret_file', type: 'file', value: '', path: 'C:/secret.txt', enabled: true },
      ],
    });
    useTabs.getState().patchDraft(tabId, draft);

    const persisted = serializeWorkspaceSessions(useTabs.getState().sessions);
    expect(persisted['workspace-a'].tabs[0].draft.headers).toEqual([
      { key: 'Authorization', value: '', enabled: true, description: '' },
      { key: 'X-Custom-Token', value: '', enabled: true, description: '' },
      { key: 'X-Trace', value: 'public-trace', enabled: true, description: '' },
    ]);
    expect(persisted['workspace-a'].tabs[0].draft.params).toEqual([
      { key: 'password', value: '', enabled: true, description: '' },
      { key: 'page', value: '1', enabled: true, description: '' },
    ]);
    expect(persisted['workspace-a'].tabs[0].draft.body.items).toEqual([
      { key: 'api_token', type: 'text', value: '', path: '', enabled: true },
      { key: 'secret_file', type: 'file', value: '', path: 'C:/secret.txt', enabled: true },
    ]);
    expect(JSON.stringify(persisted)).not.toContain('must-not-persist');
    expect(JSON.stringify(persisted)).not.toContain('query-password');
    expect(JSON.stringify(persisted)).not.toContain('form-token');
  });

  it('scrubs credentials while migrating legacy persisted drafts', () => {
    const draft = newDefaultRequest();
    draft.auth = { type: 'bearer', params: { token: 'legacy-auth-token' } };
    draft.headers = [
      { key: 'Authorization', value: 'Bearer legacy-header', enabled: true, description: '' },
    ];
    draft.params = [
      { key: 'access_token', value: 'legacy-query-token', enabled: true, description: '' },
    ];
    Object.assign(draft.body, {
      kind: 'urlencoded',
      items: [{ key: 'password', type: 'text', value: 'legacy-form-password', path: '', enabled: true }],
    });
    const migrated = migrateWorkspaceSessions({
      sessions: {
        'workspace-a': {
          activeId: 'legacy-tab',
          tabs: [{
            id: 'legacy-tab',
            workspaceId: 'workspace-a',
            name: 'legacy',
            draft,
            dirty: true,
            sending: true,
          }],
        },
      },
    });

    const tab = migrated.sessions?.['workspace-a'].tabs[0];
    expect(tab?.revision).toBe(0);
    expect(tab?.sending).toBe(false);
    expect(JSON.stringify(migrated)).not.toContain('legacy-auth-token');
    expect(JSON.stringify(migrated)).not.toContain('legacy-header');
    expect(JSON.stringify(migrated)).not.toContain('legacy-query-token');
    expect(JSON.stringify(migrated)).not.toContain('legacy-form-password');
  });

  it('overwrites legacy localStorage with scrubbed v2 state after hydration', async () => {
    const draft = newDefaultRequest();
    draft.params = [
      { key: 'access_token', value: 'legacy-hydrated-token', enabled: true, description: '' },
    ];
    localStorage.setItem('apirequest.workspace-sessions.v1', JSON.stringify({
      version: 1,
      state: {
        sessions: {
          'workspace-a': {
            activeId: 'legacy-hydrated-tab',
            tabs: [{
              id: 'legacy-hydrated-tab',
              workspaceId: 'workspace-a',
              name: 'legacy',
              draft,
              dirty: true,
              sending: false,
            }],
          },
        },
      },
    }));

    await useTabs.persist.rehydrate();

    const raw = localStorage.getItem('apirequest.workspace-sessions.v1') ?? '';
    expect(raw).not.toContain('legacy-hydrated-token');
    expect(JSON.parse(raw).version).toBe(2);
    expect(useTabs.getState().sessions['workspace-a'].tabs[0].draft.params[0].value).toBe('');
  });

  it('rejects stale or orphaned request completions by send id', () => {
    const tabId = useTabs.getState().openBlank('workspace-a');
    useTabs.getState().setSending(tabId, true, 'send-current');

    expect(useTabs.getState().setResponse(tabId, 'send-stale', { status: 200 } as never)).toBe(false);
    expect(useTabs.getState().setError(tabId, 'send-stale', { kind: 'network', detail: 'stale' })).toBe(false);
    let tab = useTabs.getState().sessions['workspace-a'].tabs[0];
    expect(tab.sending).toBe(true);
    expect(tab.response).toBeUndefined();
    expect(tab.error).toBeUndefined();

    expect(useTabs.getState().setResponse(tabId, 'send-current', { status: 201 } as never)).toBe(true);
    tab = useTabs.getState().sessions['workspace-a'].tabs[0];
    expect(tab.sending).toBe(false);
    expect(tab.response?.status).toBe(201);

    useTabs.getState().close(tabId);
    expect(useTabs.getState().setResponse(tabId, 'send-current', { status: 202 } as never)).toBe(false);
  });

  it('keeps a tab dirty when the draft changes while an older revision is saving', () => {
    const tabId = useTabs.getState().openBlank('workspace-a');
    useTabs.getState().patchDraft(tabId, { url: 'https://before-save.example' });
    const savingRevision = useTabs.getState().sessions['workspace-a'].tabs[0].revision;

    useTabs.getState().patchDraft(tabId, { url: 'https://edited-during-save.example' });
    useTabs.getState().markSaved(tabId, undefined, 'node-1', 'saved', savingRevision);

    const tab = useTabs.getState().sessions['workspace-a'].tabs[0];
    expect(tab.dirty).toBe(true);
    expect(tab.nodeId).toBe('node-1');
    expect(tab.name).toBe('saved');
  });

  it('detaches deleted nodes as recoverable drafts and rejects a stale save completion', () => {
    const draft = newDefaultRequest();
    draft.url = 'https://deleted.example';
    const tabId = useTabs.getState().openNode('workspace-a', 'node-deleted', 'deleted', draft);
    const savingRevision = useTabs.getState().sessions['workspace-a'].tabs[0].revision;

    useTabs.getState().detachNodes('workspace-a', ['node-deleted']);
    useTabs.getState().markSaved(tabId, 'node-deleted', 'node-deleted', 'stale saved name', savingRevision);

    const tab = useTabs.getState().sessions['workspace-a'].tabs[0];
    expect(tab.nodeId).toBeUndefined();
    expect(tab.dirty).toBe(true);
    expect(tab.name).toBe('deleted');
    expect(tab.draft.url).toBe('https://deleted.example');
  });

  it('flushes the debounced draft immediately for explicit close', () => {
    vi.useFakeTimers();
    const tabId = useTabs.getState().openBlank('workspace-a');
    useTabs.getState().patchDraft(tabId, { url: 'https://flush.example' });

    expect(localStorage.getItem('apirequest.workspace-sessions.v1')).not.toContain('flush.example');
    flushWorkspaceSessions();
    expect(localStorage.getItem('apirequest.workspace-sessions.v1')).toContain('flush.example');
    vi.useRealTimers();
  });

  it('retains a failed flush for a later retry', () => {
    vi.useFakeTimers();
    const tabId = useTabs.getState().openBlank('workspace-a');
    useTabs.getState().patchDraft(tabId, { url: 'https://retry.example' });
    const setItem = vi.spyOn(Storage.prototype, 'setItem').mockImplementationOnce(() => {
      throw new Error('storage unavailable');
    });

    expect(() => flushWorkspaceSessions()).toThrow('storage unavailable');
    expect(() => flushWorkspaceSessions()).not.toThrow();
    expect(localStorage.getItem('apirequest.workspace-sessions.v1')).toContain('retry.example');
    setItem.mockRestore();
    vi.useRealTimers();
  });
});
