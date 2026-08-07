// 轻量自定义下拉框：替换原生 select，美化 option 列表样式
import { useState, useRef, useEffect, type ReactNode } from 'react';

interface Option {
  value: string;
  label: string;
}

interface Props {
  value: string;
  options: Option[];
  onChange(value: string): void;
  className?: string;
  placeholder?: string;
  title?: string;
  children?: ReactNode; // 右侧额外内容（如"管理"按钮）
}

export default function Dropdown({
  value,
  options,
  onChange,
  className = '',
  placeholder = '',
  title,
  children,
}: Props) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener('mousedown', onDown);
    return () => document.removeEventListener('mousedown', onDown);
  }, [open]);

  const selected = options.find((o) => o.value === value);
  const display = selected?.label ?? placeholder ?? '';

  return (
    <div ref={ref} className={`relative flex items-center ${className}`}>
      <button
        type="button"
        className="tidy flex items-center gap-1 border border-gray-200 rounded px-2 py-1 text-xs bg-white hover:border-gray-300 focus:border-blue-400 focus:outline-none min-w-0"
        onClick={() => setOpen((v) => !v)}
        title={title}
      >
        <span className="truncate max-w-32">{display}</span>
        <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" className="text-gray-400 shrink-0">
          <polyline points="6 9 12 15 18 9" />
        </svg>
      </button>
      {children}
      {open && (
        <div className="absolute top-full left-0 mt-1 min-w-full bg-white border border-gray-200 rounded-lg shadow-lg py-1 z-50 max-h-60 overflow-auto">
          {options.map((opt) => (
            <button
              key={opt.value}
              type="button"
              className={`w-full text-left px-3 py-1.5 text-xs whitespace-nowrap hover:bg-blue-50 ${
                opt.value === value ? 'text-blue-600 font-medium bg-blue-50/50' : 'text-gray-700'
              }`}
              onClick={() => {
                onChange(opt.value);
                setOpen(false);
              }}
            >
              {opt.label}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
