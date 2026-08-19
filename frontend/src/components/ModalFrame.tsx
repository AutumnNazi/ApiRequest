import { useEffect, useRef, type ReactNode } from 'react';

interface Props {
  children: ReactNode;
  className: string;
  onClose(): void;
  onEscape?(): void;
  titleId?: string;
  ariaLabel?: string;
  closeOnBackdrop?: boolean;
}

const focusableSelector = [
  'a[href]',
  'button:not([disabled])',
  'input:not([disabled])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(',');

export default function ModalFrame({
  children,
  className,
  onClose,
  onEscape,
  titleId,
  ariaLabel,
  closeOnBackdrop = true,
}: Props) {
  const frameRef = useRef<HTMLDivElement>(null);
  const onCloseRef = useRef(onClose);
  const onEscapeRef = useRef(onEscape ?? onClose);
  onCloseRef.current = onClose;
  onEscapeRef.current = onEscape ?? onClose;

  useEffect(() => {
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const animationFrame = requestAnimationFrame(() => {
      frameRef.current?.querySelector<HTMLElement>(focusableSelector)?.focus();
    });
    const onKeyDown = (event: KeyboardEvent) => {
      const openDialogs = Array.from(
        document.querySelectorAll<HTMLElement>(
          '[role="dialog"]:not([aria-hidden="true"]), [role="alertdialog"]:not([aria-hidden="true"])',
        ),
      );
      if (openDialogs.at(-1) !== frameRef.current) return;
      if (event.key === 'Escape') {
        event.preventDefault();
        event.stopPropagation();
        onEscapeRef.current();
        return;
      }
      if (event.key !== 'Tab' || !frameRef.current) return;
      const focusable = Array.from(frameRef.current.querySelectorAll<HTMLElement>(focusableSelector));
      if (focusable.length === 0) {
        event.preventDefault();
        return;
      }
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
    document.addEventListener('keydown', onKeyDown, true);
    return () => {
      cancelAnimationFrame(animationFrame);
      document.removeEventListener('keydown', onKeyDown, true);
      previous?.focus();
    };
  }, []);

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/30 px-4"
      onClick={(event) => {
        if (closeOnBackdrop && event.target === event.currentTarget) onCloseRef.current();
      }}
    >
      <div
        ref={frameRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-label={ariaLabel}
        className={className}
      >
        {children}
      </div>
    </div>
  );
}
