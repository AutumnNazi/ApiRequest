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

// \u89e6\u53d1\u76ee\u5f55\u67e5\u627e\u7684\u5b57\u7b26\uff1aCJK \u7edf\u4e00\u6c49\u5b57\uff0c\u5916\u52a0\u4e2d\u6587\u5168\u89d2\u6807\u70b9\uff08\u3002\uff0c\u3001\uff1a\uff1b\uff01\uff1f\u300c\u300d\u300e\u300f\uff08\uff09\u7b49\uff09\u3002
// \u53ea\u542b\u5168\u89d2\u6807\u70b9\u7684\u952e\uff08\u5982 '{message}\uff1a{detail}'\uff09\u4e5f\u5fc5\u987b\u80fd\u547d\u4e2d\uff0c\u5426\u5219\u82f1\u6587\u73af\u5883\u4f1a\u6b8b\u7559\u5168\u89d2\u7b26\u53f7\u3002
const CJK_TEXT = /[\u3400-\u9fff\u3000-\u303f\uff01-\uff65]/;

export function translateExact(value: string): string {
  if (useLocale.getState().locale === 'zh-CN' || !CJK_TEXT.test(value)) return value;
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
