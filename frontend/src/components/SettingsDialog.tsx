// 应用设置弹窗：代理 + TLS + WebDAV 同步配置
import { useEffect, useState } from 'react';
import {
  getProxySettings,
  setProxySettings,
  getTLSSettings,
  setTLSSettings,
  getSyncConfig,
  setSyncConfig,
  toAppError,
  type ProxySettings,
  type TLSSettings,
  type SyncDavConfig,
} from '../ipc';

interface Props {
  onClose(): void;
}

export default function SettingsDialog({ onClose }: Props) {
  const [proxy, setProxy] = useState<ProxySettings>({ mode: 'system' });
  const [tls, setTls] = useState<TLSSettings>({});
  const [dav, setDav] = useState<Partial<SyncDavConfig>>({});
  const [msg, setMsg] = useState('');
  const [error, setError] = useState('');

  useEffect(() => {
    getProxySettings().then(setProxy).catch(() => {});
    getTLSSettings().then(setTls).catch(() => {});
    getSyncConfig().then(setDav).catch(() => {});
  }, []);

  const save = async () => {
    setError('');
    setMsg('');
    try {
      await setProxySettings(proxy);
      await setTLSSettings(tls);
      await setSyncConfig(dav);
      setMsg('已保存并生效');
      setTimeout(() => setMsg(''), 1500);
    } catch (e) {
      setError(toAppError(e).detail);
    }
  };

  return (
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50" onClick={onClose}>
      <div
        className="bg-white rounded-lg shadow-xl w-[520px] max-h-[85vh] flex flex-col"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center px-4 py-3 border-b">
          <h2 className="font-semibold text-sm">设置</h2>
          <button className="ml-auto text-gray-400 hover:text-gray-700" onClick={onClose}>
            ×
          </button>
        </div>
        <div className="p-4 space-y-4 text-sm overflow-auto">
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
          <div>
            <div className="text-gray-600 mb-2">TLS 证书</div>
            <div className="space-y-2 pl-1">
              {(
                [
                  ['caCertPath', '自定义 CA 证书（PEM，追加信任）'],
                  ['clientCertPath', '客户端证书（mTLS，PEM）'],
                  ['clientKeyPath', '客户端私钥（PEM）'],
                ] as const
              ).map(([key, label]) => (
                <div key={key}>
                  <label className="text-xs text-gray-500">{label}</label>
                  <input
                    className="w-full border rounded px-2 py-1 font-mono text-xs"
                    placeholder="留空 = 不使用"
                    value={tls[key] ?? ''}
                    onChange={(e) => setTls({ ...tls, [key]: e.target.value })}
                  />
                </div>
              ))}
            </div>
          </div>
          <div>
            <div className="text-gray-600 mb-2">WebDAV 同步（可选）</div>
            <div className="space-y-2 pl-1">
              <div>
                <label className="text-xs text-gray-500">服务器地址</label>
                <input
                  className="w-full border rounded px-2 py-1 font-mono text-xs"
                  placeholder="https://dav.jianguoyun.com/dav/  或  https://nextcloud.example.com/remote.php/dav/files/USER/"
                  value={dav.url ?? ''}
                  onChange={(e) => setDav({ ...dav, url: e.target.value })}
                />
              </div>
              <div className="flex gap-2">
                <div className="flex-1">
                  <label className="text-xs text-gray-500">用户名</label>
                  <input
                    className="w-full border rounded px-2 py-1 font-mono text-xs"
                    value={dav.username ?? ''}
                    onChange={(e) => setDav({ ...dav, username: e.target.value })}
                  />
                </div>
                <div className="flex-1">
                  <label className="text-xs text-gray-500">密码 / 应用授权码</label>
                  <input
                    type="password"
                    className="w-full border rounded px-2 py-1 font-mono text-xs"
                    value={dav.password ?? ''}
                    onChange={(e) => setDav({ ...dav, password: e.target.value })}
                  />
                </div>
              </div>
              <label className="flex items-center gap-2 text-xs text-gray-600">
                <input
                  type="checkbox"
                  checked={dav.omitSecrets ?? false}
                  onChange={(e) => setDav({ ...dav, omitSecrets: e.target.checked })}
                />
                不上传密钥变量的值（其他设备各自本地维护密钥）
              </label>
              <p className="text-xs text-gray-400">
                快照存于远端 ApiRequest/ 目录，实体级"最后写入优先"合并；顶栏 ⇅ 手动触发。
              </p>
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
