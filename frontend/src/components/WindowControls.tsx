// 窗口控制按钮组：最小化 / 最大化(切换) / 关闭。用于无边框窗口的自绘标题栏。
import { useEffect, useState } from 'react';
import {
  WindowMinimise,
  WindowToggleMaximise,
  WindowIsMaximised,
  Quit,
} from '../../wailsjs/runtime/runtime';
import { noDragRegion } from '../titlebar';

export default function WindowControls() {
  const [maximised, setMaximised] = useState(false);

  useEffect(() => {
    let alive = true;
    void WindowIsMaximised()
      .then((m) => alive && setMaximised(m))
      .catch(() => {});
    return () => {
      alive = false;
    };
  }, []);

  return (
    <div className="flex items-center -mr-3 h-10" style={noDragRegion}>
      <button
        className="h-full px-3 text-gray-500 hover:bg-gray-100 hover:text-gray-800"
        onClick={() => WindowMinimise()}
        title="最小化"
      >
        ─
      </button>
      <button
        className="h-full px-3 text-gray-500 hover:bg-gray-100 hover:text-gray-800"
        onClick={() => {
          WindowToggleMaximise();
          setMaximised((m) => !m);
        }}
        title={maximised ? '还原' : '最大化'}
      >
        {maximised ? '❐' : '□'}
      </button>
      <button
        className="h-full px-3 text-gray-500 hover:bg-red-500 hover:text-white"
        onClick={() => Quit()}
        title="关闭"
      >
        ✕
      </button>
    </div>
  );
}