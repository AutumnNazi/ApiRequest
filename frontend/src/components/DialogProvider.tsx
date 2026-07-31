import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react';
import { translate, useLocale } from '../i18n/locale';

type DialogKind = 'alert' | 'confirm' | 'prompt';
type ToastKind = 'info' | 'success' | 'error';

interface DialogRequest {
  id: number;
  kind: DialogKind;
  title: string;
  message: string;
  defaultValue?: string;
  placeholder?: string;
  confirmLabel?: string;
  cancelLabel?: string;
  resolve(value: string | boolean | null): void;
}

interface DialogOptions {
  title?: string;
  confirmLabel?: string;
  cancelLabel?: string;
}

interface PromptOptions extends DialogOptions {
  defaultValue?: string;
  placeholder?: string;
}

interface DialogContextValue {
  alert(message: string, options?: DialogOptions): Promise<void>;
  confirm(message: string, options?: DialogOptions): Promise<boolean>;
  prompt(message: string, options?: PromptOptions): Promise<string | null>;
  toast(message: string, kind?: ToastKind): void;
}

interface ToastItem {
  id: number;
  message: string;
  kind: ToastKind;
}

const DialogContext = createContext<DialogContextValue | null>(null);
let nextId = 1;

export function DialogProvider({ children }: { children: ReactNode }) {
  useLocale((state) => state.locale);
  const [active, setActive] = useState<DialogRequest | null>(null);
  const [promptValue, setPromptValue] = useState('');
  const [toasts, setToasts] = useState<ToastItem[]>([]);
  const queue = useRef<DialogRequest[]>([]);
  const dialogRef = useRef<HTMLDivElement>(null);

  const enqueue = useCallback(
    (request: Omit<DialogRequest, 'id'>) => {
      const item = { ...request, id: nextId++ };
      setActive((current) => {
        if (current) {
          queue.current.push(item);
          return current;
        }
        return item;
      });
    },
    [],
  );

  const alert = useCallback(
    (message: string, options: DialogOptions = {}) =>
      new Promise<void>((resolve) => {
        enqueue({
          kind: 'alert',
          title: options.title ?? '提示',
          message,
          confirmLabel: options.confirmLabel,
          resolve: () => resolve(),
        });
      }),
    [enqueue],
  );

  const confirm = useCallback(
    (message: string, options: DialogOptions = {}) =>
      new Promise<boolean>((resolve) => {
        enqueue({
          kind: 'confirm',
          title: options.title ?? '确认操作',
          message,
          confirmLabel: options.confirmLabel,
          cancelLabel: options.cancelLabel,
          resolve: (value) => resolve(value === true),
        });
      }),
    [enqueue],
  );

  const prompt = useCallback(
    (message: string, options: PromptOptions = {}) =>
      new Promise<string | null>((resolve) => {
        enqueue({
          kind: 'prompt',
          title: options.title ?? '输入',
          message,
          defaultValue: options.defaultValue,
          placeholder: options.placeholder,
          confirmLabel: options.confirmLabel,
          cancelLabel: options.cancelLabel,
          resolve: (value) => resolve(typeof value === 'string' ? value : null),
        });
      }),
    [enqueue],
  );

  const toast = useCallback((message: string, kind: ToastKind = 'info') => {
    const id = nextId++;
    setToasts((items) => [...items, { id, message, kind }]);
    window.setTimeout(() => setToasts((items) => items.filter((item) => item.id !== id)), 3500);
  }, []);

  const finish = useCallback(
    (value: string | boolean | null) => {
      if (!active) return;
      active.resolve(value);
      setActive(queue.current.shift() ?? null);
    },
    [active],
  );

  useEffect(() => {
    if (!active) return;
    setPromptValue(active.defaultValue ?? '');
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const frame = requestAnimationFrame(() => {
      const first = dialogRef.current?.querySelector<HTMLElement>(
        'input, button:not([disabled]), textarea, select, [tabindex]:not([tabindex="-1"])',
      );
      first?.focus();
      if (first instanceof HTMLInputElement) first.select();
    });
    return () => {
      cancelAnimationFrame(frame);
      previous?.focus();
    };
  }, [active]);

  useEffect(() => {
    if (!active) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault();
        finish(active.kind === 'alert' ? true : null);
        return;
      }
      if (event.key !== 'Tab' || !dialogRef.current) return;
      const focusable = Array.from(
        dialogRef.current.querySelectorAll<HTMLElement>(
          'input, button:not([disabled]), textarea, select, [tabindex]:not([tabindex="-1"])',
        ),
      );
      if (focusable.length === 0) return;
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    };
    document.addEventListener('keydown', onKeyDown);
    return () => document.removeEventListener('keydown', onKeyDown);
  }, [active, finish]);

  const value = useMemo(() => ({ alert, confirm, prompt, toast }), [alert, confirm, prompt, toast]);
  const titleId = active ? `dialog-title-${active.id}` : undefined;
  const descriptionId = active ? `dialog-description-${active.id}` : undefined;

  return (
    <DialogContext.Provider value={value}>
      {children}
      {active && (
        <div className="fixed inset-0 z-[100] flex items-center justify-center bg-black/35 px-4">
          <div
            ref={dialogRef}
            role={active.kind === 'alert' ? 'alertdialog' : 'dialog'}
            aria-modal="true"
            aria-labelledby={titleId}
            aria-describedby={descriptionId}
            className="w-full max-w-md rounded-lg border bg-white shadow-xl"
          >
            <div className="border-b px-4 py-3">
              <h2 id={titleId} className="text-sm font-semibold text-gray-900">
                {translate(active.title)}
              </h2>
            </div>
            <form
              onSubmit={(event) => {
                event.preventDefault();
                finish(active.kind === 'prompt' ? promptValue : true);
              }}
            >
              <div className="space-y-3 px-4 py-4">
                <p id={descriptionId} className="whitespace-pre-wrap text-sm text-gray-700">
                  {translate(active.message)}
                </p>
                {active.kind === 'prompt' && (
                  <input
                    className="w-full rounded border px-2.5 py-2 text-sm outline-none focus:border-blue-500 focus:ring-1 focus:ring-blue-500"
                    value={promptValue}
                    placeholder={active.placeholder ? translate(active.placeholder) : undefined}
                    onChange={(event) => setPromptValue(event.target.value)}
                  />
                )}
              </div>
              <div className="flex justify-end gap-2 border-t px-4 py-3">
                {active.kind !== 'alert' && (
                  <button
                    type="button"
                    className="rounded border px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-50"
                    onClick={() => finish(null)}
                  >
                    {translate(active.cancelLabel ?? '取消')}
                  </button>
                )}
                <button
                  type="submit"
                  className="rounded bg-blue-600 px-3 py-1.5 text-sm text-white hover:bg-blue-700"
                >
                  {translate(active.confirmLabel ?? '确定')}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
      <div className="pointer-events-none fixed right-4 top-4 z-[120] flex w-[min(360px,calc(100vw-2rem))] flex-col gap-2" aria-live="polite">
        {toasts.map((item) => (
          <div
            key={item.id}
            role={item.kind === 'error' ? 'alert' : 'status'}
            className={`rounded border bg-white px-3 py-2 text-sm shadow-lg ${
              item.kind === 'error'
                ? 'border-red-200 text-red-700'
                : item.kind === 'success'
                  ? 'border-green-200 text-green-700'
                  : 'border-gray-200 text-gray-700'
            }`}
          >
            {translate(item.message)}
          </div>
        ))}
      </div>
    </DialogContext.Provider>
  );
}

export function useDialog(): DialogContextValue {
  const context = useContext(DialogContext);
  if (!context) throw new Error('useDialog must be used inside DialogProvider');
  return context;
}
