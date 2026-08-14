export async function closeTabsSequentially<T>(
  tabs: readonly T[],
  closeTab: (tab: T) => Promise<boolean>,
): Promise<boolean> {
  for (const tab of tabs) {
    if (!(await closeTab(tab))) return false;
  }
  return true;
}

interface CloseableTab {
  id: string;
  revision: number;
  dirty: boolean;
  sendId?: string;
}

type CloseCommitResult<T> =
  | { status: 'closed'; tab: T }
  | { status: 'changed' }
  | { status: 'missing' };

interface CloseTabLifecycle<T extends CloseableTab> {
  read(tabId: string): T | undefined;
  confirmDiscard(tab: T): Promise<boolean>;
  cancel(sendId: string): Promise<unknown>;
  commit(expected: T): CloseCommitResult<T>;
}

export async function closeTabSafely<T extends CloseableTab>(
  tabId: string,
  lifecycle: CloseTabLifecycle<T>,
): Promise<{ continue: boolean; closed?: T }> {
  let confirmedRevision: number | undefined;
  for (;;) {
    const current = lifecycle.read(tabId);
    if (!current) return { continue: true };

    if (current.dirty && confirmedRevision !== current.revision) {
      if (!(await lifecycle.confirmDiscard(current))) return { continue: false };
      confirmedRevision = current.revision;
      continue;
    }

    if (current.sendId) await lifecycle.cancel(current.sendId);

    const committed = lifecycle.commit(current);
    if (committed.status === 'changed') continue;
    if (committed.status === 'missing') return { continue: true };
    return { continue: true, closed: committed.tab };
  }
}
