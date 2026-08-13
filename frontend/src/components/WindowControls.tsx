// 窗口控制按钮组：最小化 / 最大化(切换) / 关闭。用于无边框窗口的自绘标题栏。
import { useEffect, useState } from 'react';
import {
  WindowMinimise,
  WindowToggleMaximise,
  WindowIsMaximised,
} from '../../wailsjs/runtime/runtime';
import { noDragRegion } from '../titlebar';

const controlClass =
  'inline-flex h-10 w-11 shrink-0 items-center justify-center p-0 text-gray-500 transition-colors duration-150 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-gray-400';

export default function WindowControls({ onClose }: { onClose(): void }) {
  const [maximised, setMaximised] = useState(false);

  useEffect(() => {
    let alive = true;
    const refreshMaximised = () => {
      void WindowIsMaximised()
        .then((m) => alive && setMaximised(m))
        .catch(() => {});
    };
    refreshMaximised();
    window.addEventListener('resize', refreshMaximised);
    return () => {
      alive = false;
      window.removeEventListener('resize', refreshMaximised);
    };
  }, []);

  const toggleMaximise = () => {
    WindowToggleMaximise();
    setMaximised((current) => !current);
  };

  return (
    <div
      className="-mr-3 flex h-10 items-center"
      data-no-drag
      style={noDragRegion}
      role="group"
      aria-label="窗口控制"
    >
      <button
        type="button"
        className={`${controlClass} hover:bg-gray-100 hover:text-gray-800`}
        onClick={() => WindowMinimise()}
        title="最小化"
        aria-label="最小化"
      >
        <span aria-hidden="true" className="block h-px w-3 bg-current" />
      </button>
      <button
        type="button"
        className={`${controlClass} hover:bg-gray-100 hover:text-gray-800`}
        onClick={toggleMaximise}
        title={maximised ? '还原' : '最大化'}
        aria-label={maximised ? '还原' : '最大化'}
      >
        {maximised ? (
          <span aria-hidden="true" className="relative block h-3 w-3">
            <span className="absolute right-0 top-0 h-[9px] w-[9px] border-r border-t border-current" />
            <span className="absolute bottom-0 left-0 h-[9px] w-[9px] border border-current" />
          </span>
        ) : (
          <span aria-hidden="true" className="block h-3 w-3 border border-current" />
        )}
      </button>
      <button
        type="button"
        className={`${controlClass} hover:bg-red-500 hover:text-white`}
        onClick={onClose}
        title="关闭"
        aria-label="关闭"
      >
        <span aria-hidden="true" className="relative block h-[14px] w-[14px]">
          <span className="absolute left-0 top-1/2 block h-px w-[14px] -translate-y-1/2 rotate-45 bg-current" />
          <span className="absolute left-0 top-1/2 block h-px w-[14px] -translate-y-1/2 -rotate-45 bg-current" />
        </span>
      </button>
    </div>
  );
}
