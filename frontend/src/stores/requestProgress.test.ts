import { beforeEach, describe, expect, it } from 'vitest';
import { useRequestProgress } from './requestProgress';

describe('Request Progress store', () => {
  beforeEach(() => {
    useRequestProgress.setState({ bySendId: {} });
  });

  it('tracks only active sends and ignores orphaned events', () => {
    const store = useRequestProgress.getState();
    store.start('active-send');
    store.update({
      sendId: 'orphan-send',
      phase: 'downloading',
      bytesReceived: 64,
      totalBytes: 128,
    });
    store.update({
      sendId: 'active-send',
      phase: 'downloading',
      bytesReceived: 96,
      totalBytes: 128,
    });

    expect(useRequestProgress.getState().bySendId).toEqual({
      'active-send': {
        sendId: 'active-send',
        phase: 'downloading',
        bytesReceived: 96,
        totalBytes: 128,
      },
    });
  });

  it('drops terminal state so delayed events cannot recreate it', () => {
    const store = useRequestProgress.getState();
    store.start('send-1');
    store.clear('send-1');
    store.update({
      sendId: 'send-1',
      phase: 'done',
      bytesReceived: 128,
      totalBytes: 128,
    });

    expect(useRequestProgress.getState().bySendId).toEqual({});
  });
});
