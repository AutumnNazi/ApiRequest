// 应用设置：语言、Secret Vault、代理、TLS 与 WebDAV。
import { useEffect, useState } from 'react';
import {
  getProxySettings,
  setProxySettings,
  getTLSSettings,
  setTLSSettings,
  getSyncConfig,
  setSyncConfig,
  getVaultStatus,
  unlockVault,
  lockVault,
  openReleasePage,
  openNativeFile,
  toAppError,
  type ProxySettings,
  type TLSSettings,
  type SyncDavConfig,
  type VaultStatus,
} from '../ipc';
import { useLocale, Verbatim, type Locale } from '../i18n/locale';

interface Props {
  onClose(): void;
}

const tlsFields: Array<[keyof TLSSettings, string, string]> = [
  ['caCertPath', '自定义 CA 证书（PEM，追加信任）', '选择 CA 证书'],
  ['clientCertPath', '客户端证书（mTLS，PEM）', '选择客户端证书'],
  ['clientKeyPath', '客户端私钥（PEM）', '选择客户端私钥'],
];

export default function SettingsDialog({ onClose }: Props) {
  const locale = useLocale((state) => state.locale);
  const setLocale = useLocale((state) => state.setLocale);
  const [proxy, setProxy] = useState<ProxySettings>({ mode: 'system' });
  const [tls, setTls] = useState<TLSSettings>({});
  const [dav, setDav] = useState<Partial<SyncDavConfig>>({});
  const [vault, setVault] = useState<VaultStatus | null>(null);
  const [vaultPassword, setVaultPassword] = useState('');
  const [vaultBusy, setVaultBusy] = useState(false);
  const [msg, setMsg] = useState('');
  const [error, setError] = useState('');

  useEffect(() => {
    Promise.all([getProxySettings(), getTLSSettings(), getSyncConfig(), getVaultStatus()])
      .then(([nextProxy, nextTls, nextDav, nextVault]) => {
        setProxy(nextProxy);
        setTls(nextTls);
        setDav(nextDav);
        setVault(nextVault);
      })
      .catch((cause) => setError(toAppError(cause).detail));
  }, []);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose();
    };
    document.addEventListener('keydown', onKeyDown);
    return () => document.removeEventListener('keydown', onKeyDown);
  }, [onClose]);

  const save = async () => {
    setError('');
    setMsg('');
    try {
      await setProxySettings(proxy);
      await setTLSSettings(tls);
      await setSyncConfig(dav);
      setDav(await getSyncConfig());
      setVault(await getVaultStatus());
      setMsg('已保存并生效');
      window.setTimeout(() => setMsg(''), 1500);
    } catch (cause) {
      setError(toAppError(cause).detail);
    }
  };

  const chooseCertificate = async (key: keyof TLSSettings, title: string) => {
    try {
      const path = await openNativeFile(title);
      if (path) setTls((current) => ({ ...current, [key]: path }));
    } catch (cause) {
      setError(toAppError(cause).detail);
    }
  };

  const handleUnlock = async () => {
    if (!vaultPassword.trim() || vaultBusy) return;
    setVaultBusy(true);
    setError('');
    try {
      setVault(await unlockVault(vaultPassword));
      setVaultPassword('');
      setMsg('Secret Vault 已解锁');
    } catch (cause) {
      setError(toAppError(cause).detail);
    } finally {
      setVaultBusy(false);
    }
  };

  const handleLock = async () => {
    setVaultBusy(true);
    try {
      setVault(await lockVault());
      setMsg('Secret Vault 已锁定');
    } finally {
      setVaultBusy(false);
    }
  };

  const vaultLabel = !vault
    ? '检测中…'
    : vault.mode === 'keyring'
      ? '系统密钥链'
      : vault.fileUnlocked
        ? '加密文件（已解锁）'
        : '加密文件（已锁定）';

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/30 px-4" onClick={onClose}>
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="settings-title"
        className="flex max-h-[88vh] w-[600px] max-w-full flex-col rounded-lg bg-white shadow-xl"
        onClick={(event) => event.stopPropagation()}
      >
        <div className="flex items-center border-b px-4 py-3">
          <h2 id="settings-title" className="text-sm font-semibold">设置</h2>
          <button className="ml-auto text-gray-400 hover:text-gray-700" onClick={onClose} aria-label="关闭">×</button>
        </div>
        <div className="space-y-5 overflow-auto p-4 text-sm">
          <section>
            <h3 className="mb-2 text-gray-600">界面</h3>
            <label className="flex items-center gap-3 pl-1">
              <span className="w-24 text-xs text-gray-500">语言</span>
              <select
                className="rounded border px-2 py-1.5 text-sm"
                value={locale}
                onChange={(event) => setLocale(event.target.value as Locale)}
              >
                <option value="zh-CN">简体中文</option>
                <option value="en">English</option>
              </select>
            </label>
            <button className="mt-2 rounded border px-3 py-1.5 text-xs hover:bg-gray-50" onClick={openReleasePage}>
              检查更新并打开下载页
            </button>
          </section>

          <section className="border-t pt-4">
            <div className="mb-2 flex items-center gap-2">
              <h3 className="text-gray-600">Secret Vault</h3>
              <span className={`text-xs ${vault?.canStore ? 'text-green-600' : 'text-amber-600'}`}>{vaultLabel}</span>
            </div>
            {vault && !vault.keyringAvailable && !vault.fileUnlocked && (
              <div className="flex gap-2 pl-1">
                <input
                  type="password"
                  className="min-w-0 flex-1 rounded border px-2 py-1.5 text-sm"
                  placeholder={vault.fileExists ? '输入主密码' : '设置新的主密码'}
                  value={vaultPassword}
                  onChange={(event) => setVaultPassword(event.target.value)}
                  onKeyDown={(event) => {
                    if (event.key === 'Enter') void handleUnlock();
                  }}
                />
                <button
                  className="rounded border px-3 py-1.5 text-sm hover:bg-gray-50 disabled:opacity-50"
                  disabled={!vaultPassword.trim() || vaultBusy}
                  onClick={handleUnlock}
                >
                  {vault.fileExists ? '解锁' : '创建并解锁'}
                </button>
              </div>
            )}
            {vault?.fileUnlocked && !vault.keyringAvailable && (
              <button className="rounded border px-3 py-1.5 text-sm hover:bg-gray-50 disabled:opacity-50" disabled={vaultBusy} onClick={handleLock}>
                锁定 Vault
              </button>
            )}
            <p className="mt-2 text-xs text-gray-400">
              凭据优先存入系统密钥链；不可用时使用 Argon2id 与 AES-GCM 加密文件。主密码不会写入磁盘。
            </p>
          </section>

          <section className="border-t pt-4">
            <h3 className="mb-2 text-gray-600">代理</h3>
            <div className="space-y-2 pl-1">
              {([
                ['system', '使用系统代理（环境变量 HTTP_PROXY 等）'],
                ['manual', '手动配置'],
                ['none', '直连（不使用代理）'],
              ] as const).map(([mode, label]) => (
                <label key={mode} className="flex items-center gap-2">
                  <input type="radio" checked={proxy.mode === mode} onChange={() => setProxy({ ...proxy, mode })} />
                  {label}
                </label>
              ))}
              {proxy.mode === 'manual' && (
                <input
                  className="mt-1 w-full rounded border px-2 py-1 font-mono text-xs"
                  placeholder="http://127.0.0.1:7890 或 socks5://127.0.0.1:1080"
                  value={proxy.url ?? ''}
                  onChange={(event) => setProxy({ ...proxy, url: event.target.value })}
                />
              )}
            </div>
          </section>

          <section className="border-t pt-4">
            <h3 className="mb-2 text-gray-600">TLS 证书</h3>
            <div className="space-y-2 pl-1">
              {tlsFields.map(([key, label, title]) => (
                <div key={key}>
                  <label className="text-xs text-gray-500">{label}</label>
                  <div className="flex gap-2">
                    <input
                      className="min-w-0 flex-1 rounded border px-2 py-1 font-mono text-xs"
                      placeholder="留空 = 不使用"
                      value={tls[key] ?? ''}
                      onChange={(event) => setTls({ ...tls, [key]: event.target.value })}
                    />
                    <button className="rounded border px-3 py-1 text-xs hover:bg-gray-50" onClick={() => void chooseCertificate(key, title)}>
                      浏览…
                    </button>
                  </div>
                </div>
              ))}
            </div>
          </section>

          <section className="border-t pt-4">
            <h3 className="mb-2 text-gray-600">WebDAV 同步（可选）</h3>
            <div className="space-y-2 pl-1">
              <div>
                <label className="text-xs text-gray-500">服务器地址</label>
                <input
                  className="w-full rounded border px-2 py-1 font-mono text-xs"
                  placeholder="https://dav.example.com/remote.php/dav/files/USER/"
                  value={dav.url ?? ''}
                  onChange={(event) => setDav({ ...dav, url: event.target.value })}
                />
              </div>
              <div className="flex gap-2">
                <div className="flex-1">
                  <label className="text-xs text-gray-500">用户名</label>
                  <input className="w-full rounded border px-2 py-1 font-mono text-xs" value={dav.username ?? ''} onChange={(event) => setDav({ ...dav, username: event.target.value })} />
                </div>
                <div className="flex-1">
                  <label className="text-xs text-gray-500">密码 / 应用授权码</label>
                  <input
                    type="password"
                    className="w-full rounded border px-2 py-1 font-mono text-xs"
                    placeholder={dav.passwordSet ? '已保存；留空则保持不变' : '未设置'}
                    value={dav.password ?? ''}
                    onChange={(event) => setDav({ ...dav, password: event.target.value, clearPassword: false })}
                  />
                  {dav.passwordSet && !dav.clearPassword && (
                    <button
                      className="mt-1 text-xs text-red-600 hover:text-red-700"
                      onClick={() => setDav({ ...dav, password: '', passwordSet: false, clearPassword: true })}
                    >
                      清除已保存密码
                    </button>
                  )}
                  {dav.clearPassword && <span className="mt-1 block text-xs text-amber-600">保存后清除密码</span>}
                </div>
              </div>
              <label className="flex items-center gap-2 text-xs text-gray-600">
                <input type="checkbox" checked={dav.omitSecrets ?? false} onChange={(event) => setDav({ ...dav, omitSecrets: event.target.checked })} />
                不上传密钥变量的值（其他设备各自本地维护密钥）
              </label>
              <p className="text-xs text-gray-400">快照存于远端 ApiRequest/ 目录，实体级“最后写入优先”合并；顶栏手动触发同步。</p>
            </div>
          </section>

          {error && <p className="text-xs text-red-600" role="alert"><Verbatim value={error} /></p>}
          {msg && <p className="text-xs text-green-600" role="status">{msg}</p>}
        </div>
        <div className="flex justify-end gap-2 border-t px-4 py-3">
          <button className="rounded bg-blue-600 px-4 py-1.5 text-sm text-white hover:bg-blue-700" onClick={save}>保存</button>
        </div>
      </div>
    </div>
  );
}
