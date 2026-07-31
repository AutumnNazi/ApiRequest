import { create } from 'zustand';
import { createJSONStorage, persist } from 'zustand/middleware';
import type { ReactNode } from 'react';
import { en } from './messages.en';

export type Locale = 'zh-CN' | 'en';

interface LocaleState {
  locale: Locale;
  setLocale(locale: Locale): void;
}

export const useLocale = create<LocaleState>()(
  persist(
    (set) => ({
      locale: 'zh-CN',
      setLocale: (locale) => set({ locale }),
    }),
    {
      name: 'apirequest.locale.v1',
      storage: createJSONStorage(() => localStorage),
    },
  ),
);

export function translateExact(value: string): string {
  if (useLocale.getState().locale === 'zh-CN' || !/[\u3400-\u9fff]/.test(value)) return value;
  return en[value] ?? value;
}

export function translate(value: string): string {
  return translateExact(value);
}

// Keep user-controlled and protocol data outside the automatic JSX catalog lookup.
export function Verbatim({ value }: { value: ReactNode }): ReactNode {
  return value;
}

export function formatMessage(message: string, values: Record<string, string | number> = {}): string {
  let result = translate(message);
  for (const [key, value] of Object.entries(values)) {
    result = result.replaceAll(`{${key}}`, String(value));
  }
  return result;
}
