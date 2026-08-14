import { describe, expect, it, vi } from 'vitest';
import { closeTabSafely, closeTabsSequentially } from './tabClose';

describe('closeTabsSequentially', () => {
  it('closes in order and stops after a close is cancelled', async () => {
    let active = 0;
    let maxActive = 0;
    const closeTab = vi.fn(async (tab: string) => {
      active += 1;
      maxActive = Math.max(maxActive, active);
      await Promise.resolve();
      active -= 1;
      return tab !== 'second';
    });

    await expect(closeTabsSequentially(['first', 'second', 'third'], closeTab)).resolves.toBe(false);
    expect(closeTab.mock.calls.map(([tab]) => tab)).toEqual(['first', 'second']);
    expect(maxActive).toBe(1);
  });

  it('returns true after every tab closes', async () => {
    const closeTab = vi.fn(async () => true);

    await expect(closeTabsSequentially(['first', 'second'], closeTab)).resolves.toBe(true);
    expect(closeTab).toHaveBeenCalledTimes(2);
  });

  it('revalidates edits made while an active request is being cancelled', async () => {
    type TabState = {
      id: string;
      revision: number;
      dirty: boolean;
      sendId?: string;
    };
    let current: TabState | undefined = {
      id: 'tab-1',
      revision: 1,
      dirty: false,
      sendId: 'send-1',
    };
    const confirmDiscard = vi.fn(async () => true);
    const cancel = vi.fn(async () => {
      current = { id: 'tab-1', revision: 2, dirty: true };
    });
    const commit = vi.fn((expected: TabState) => {
      if (!current) return { status: 'missing' as const };
      if (current.revision !== expected.revision || current.sendId !== expected.sendId) {
        return { status: 'changed' as const };
      }
      const closed = current;
      current = undefined;
      return { status: 'closed' as const, tab: closed };
    });

    const result = await closeTabSafely('tab-1', {
      read: () => current,
      confirmDiscard,
      cancel,
      commit,
    });

    expect(confirmDiscard).toHaveBeenCalledOnce();
    expect(confirmDiscard).toHaveBeenCalledWith(expect.objectContaining({ revision: 2 }));
    expect(result).toEqual({ continue: true, closed: expect.objectContaining({ revision: 2 }) });
  });
});
