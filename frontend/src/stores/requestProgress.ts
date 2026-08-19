import { create } from 'zustand';
import type { RequestProgress } from '../ipc';

interface RequestProgressState {
  bySendId: Record<string, RequestProgress>;
  start(sendId: string): void;
  update(progress: RequestProgress): void;
  clear(sendId: string): void;
}

export const useRequestProgress = create<RequestProgressState>((set) => ({
  bySendId: {},

  start(sendId) {
    set((state) => ({
      bySendId: {
        ...state.bySendId,
        [sendId]: { sendId, phase: 'sending', bytesReceived: 0, totalBytes: 0 },
      },
    }));
  },

  update(progress) {
    set((state) => {
      if (!state.bySendId[progress.sendId]) return state;
      return { bySendId: { ...state.bySendId, [progress.sendId]: progress } };
    });
  },

  clear(sendId) {
    set((state) => {
      if (!state.bySendId[sendId]) return state;
      const bySendId = { ...state.bySendId };
      delete bySendId[sendId];
      return { bySendId };
    });
  },
}));
