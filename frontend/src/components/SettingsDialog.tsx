// 应用设置弹窗：代理配置
import { useEffect, useState } from 'react';
import { getProxySettings, setProxySettings, toAppError, type ProxySettings } from '../ipc';

interface Props {
  onClose(): void;
}

export default function SettingsDialog({ onClose }: Props) {
  const [proxy, setProxy] = useState<ProxySettings>({ mode: 'system' });
  const [msg, setMsg] = useState('');
  const [error, setError] = useState('');

  useEffect(() => {
    getProxySettings().then(setProxy).catch(() => {});
  }, []);

  const save = async () => {
    setError('');
    setMsg('');
    try {
      await setProxySettings(proxy);
      setMsg('已保存并生效');
      setTimeout(() => setMsg(''), 1500);
    } catch (e) {
      setError(toAppError(e).detail);
    }
  };

  return (
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50" onClick={onClose}>
      <div
        className="bg-white rounded-lg shadow-xl w-[480px] flex flex-col"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center px-4 py-3 border-b">
          <h2 className="font-semibold text-sm">设置</h2>
          <button className="ml-auto text-gray-400 hover:text-gray-700" onClick={onClose}>
            ×
          </button>
        </div>
        <div className="p-4 space-y-4 text-sm">
          <div>
            <div className="text-gray-600 mb-2">代理</div>
            <div className="space-y-2 pl-1">
              {(
                [
                  ['system', '使用系统代理（环境变量 HTTP_PROXY 等）'],
                  ['manual', '手动配置'],
                  ['none', '直连（不使用代理）'],
                ] as const
              ).map(([mode, label]) => (
                <label key={mode} className="flex items-center gap-2">
                  <input
                    type="radio"
                    checked={proxy.mode === mode}
                    onChange={() => setProxy({ ...proxy, mode })}
                  />
                  {label}
                </label>
              ))}
              {proxy.mode === 'manual' && (
                <input
                  className="w-full border rounded px-2 py-1 font-mono text-xs mt-1"
                  placeholder="http://127.0.0.1:7890 或 socks5://127.0.0.1:1080"
                  value={proxy.url ?? ''}
                  onChange={(e) => setProxy({ ...proxy, url: e.target.value })}
                />
              )}
            </div>
          </div>
          {error && <p className="text-xs text-red-600">{error}</p>}
          {msg && <p className="text-xs text-green-600">{msg}</p>}
        </div>
        <div className="flex justify-end gap-2 px-4 py-3 border-t">
          <button
            className="bg-blue-600 text-white rounded px-4 py-1.5 text-sm hover:bg-blue-700"
            onClick={save}
          >
            保存
          </button>
        </div>
      </div>
    </div>
  );
}
