import { beforeEach, describe, expect, it } from 'vitest';
import { newDefaultRequest } from '../ipc';
import { serializeWorkspaceSessions, useTabs } from './tabs';

describe('Workspace Session store', () => {
  beforeEach(() => {
    localStorage.clear();
    useTabs.setState({ sessions: {} });
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

  it('routes progress to the tab that owns the send id', () => {
    const first = useTabs.getState().openBlank('workspace-a');
    const second = useTabs.getState().openBlank('workspace-b');
    useTabs.getState().setSending(first, true, 'send-a');
    useTabs.getState().setSending(second, true, 'send-b');
    useTabs.getState().setProgress({
      sendId: 'send-a',
      phase: 'downloading',
      bytesReceived: 64,
      totalBytes: 128,
    });

    expect(useTabs.getState().sessions['workspace-a'].tabs[0].progress?.bytesReceived).toBe(64);
    expect(useTabs.getState().sessions['workspace-b'].tabs[0].progress?.phase).toBe('sending');
  });
});
