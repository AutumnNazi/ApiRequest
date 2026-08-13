// 应用设置：左侧分类导航 + 右侧内容面板
import { useEffect, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
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
import { useLocale, Verbatim, formatMessage, type Locale } from '../i18n/locale';

interface Props {
  onClose(): void;
}

const tlsFields: Array<[keyof TLSSettings, string, string]> = [
  ['caCertPath', formatMessage('自定义 CA 证书'), formatMessage('选择 CA 证书')],
  ['clientCertPath', formatMessage('客户端证书 (mTLS)'), formatMessage('选择客户端证书')],
  ['clientKeyPath', formatMessage('客户端私钥'), formatMessage('选择客户端私钥')],
];

type Category = 'general' | 'security' | 'network' | 'sync' | 'about';

const categories: Array<[Category, string]> = [
  ['general', formatMessage('通用')],
  ['security', formatMessage('安全')],
  ['network', formatMessage('网络')],
  ['sync', formatMessage('同步')],
  ['about', formatMessage('关于')],
];

export default function SettingsDialog({ onClose }: Props) {
  const qc = useQueryClient();
  const locale = useLocale((state) => state.locale);
  const setLocale = useLocale((state) => state.setLocale);
  const [cat, setCat] = useState<Category>('general');
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
      await qc.invalidateQueries({ queryKey: ['syncConfig'] });
      setVault(await getVaultStatus());
      setMsg(formatMessage('已保存并生效'));
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
      setMsg(formatMessage('Secret Vault 已解锁'));
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
      setMsg(formatMessage('Secret Vault 已锁定'));
    } finally {
      setVaultBusy(false);
    }
  };

  const vaultLabel = !vault
    ? formatMessage('检测中…')
    : vault.mode === 'keyring'
      ? formatMessage('系统密钥链')
      : vault.fileUnlocked
        ? formatMessage('加密文件（已解锁）')
        : formatMessage('加密文件（已锁定）');

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/30 px-4" onClick={onClose}>
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="settings-title"
        className="flex h-[520px] w-[680px] max-w-full rounded-lg bg-white shadow-xl overflow-hidden"
        onClick={(event) => event.stopPropagation()}
      >
        {/* 左侧导航 */}
        <nav className="w-36 shrink-0 border-r bg-gray-50 p-2">
          <h2 id="settings-title" className="px-2 py-2 text-sm font-semibold text-gray-700">{formatMessage('设置')}</h2>
          {categories.map(([key, label]) => (
            <button
              key={key}
              className={`w-full text-left rounded px-3 py-1.5 text-sm ${
                cat === key ? 'bg-white font-medium text-gray-900 shadow-sm' : 'text-gray-500 hover:text-gray-800'
              }`}
              onClick={() => setCat(key)}
            >
              {label}
            </button>
          ))}
        </nav>

        {/* 右侧内容 */}
        <div className="flex min-w-0 flex-1 flex-col">
          <div className="min-h-0 flex-1 overflow-auto p-5 text-sm">
            {/* 通用 */}
            {cat === 'general' && (
              <div className="space-y-4">
                <div>
                  <label className="mb-1.5 block text-xs font-medium text-gray-500">{formatMessage('语言')}</label>
                  <select
                    className="rounded border border-gray-200 px-3 py-2 text-sm hover:border-gray-300 focus:border-blue-400 focus:outline-none"
                    value={locale}
                    onChange={(event) => setLocale(event.target.value as Locale)}
                  >
                    <option value="zh-CN">简体中文</option>
                    <option value="en">English</option>
                  </select>
                </div>
              </div>
            )}

            {/* 安全 */}
            {cat === 'security' && (
              <div className="space-y-4">
                {/* 状态卡片 */}
                <div className="rounded-lg border border-gray-200 bg-gray-50 p-4">
                  <div className="flex items-center justify-between">
                    <span className="text-sm font-medium text-gray-700">Secret Vault</span>
                    <span className={`rounded-full px-2 py-0.5 text-xs ${
                      vault?.canStore ? 'bg-green-100 text-green-700' : 'bg-amber-100 text-amber-700'
                    }`}>
                      {vaultLabel}
                    </span>
                  </div>
                  <div className="mt-3 space-y-1.5 text-xs text-gray-500">
                    <div className="flex justify-between">
                      <span>{formatMessage('存储方式')}</span>
                      <span className="text-gray-700">
                        {vault?.keyringAvailable ? formatMessage('系统密钥链') : formatMessage('加密文件')}
                      </span>
                    </div>
                    <div className="flex justify-between">
                      <span>{formatMessage('状态')}</span>
                      <span className={vault?.canStore ? 'text-green-600' : 'text-amber-600'}>
                        {vault?.canStore ? formatMessage('可用') : formatMessage('待解锁')}
                      </span>
                    </div>
                  </div>
                </div>

                {/* 操作区 */}
                {vault && !vault.fileUnlocked && (!vault.keyringAvailable || vault.fileExists) && (
                  <div>
                    <label className="mb-1.5 block text-xs font-medium text-gray-500">
                      {vault.keyringAvailable && vault.fileExists
                        ? formatMessage('输入主密码以解锁旧加密文件')
                        : vault.fileExists
                          ? formatMessage('输入主密码以解锁')
                          : formatMessage('设置新的主密码')}
                    </label>
                    <div className="flex gap-2">
                      <input
                        type="password"
                        className="min-w-0 flex-1 rounded border border-gray-200 px-3 py-2 text-sm focus:border-blue-400 focus:outline-none"
                        placeholder={vault.fileExists ? formatMessage('主密码') : formatMessage('设置主密码')}
                        value={vaultPassword}
                        onChange={(event) => setVaultPassword(event.target.value)}
                        onKeyDown={(event) => {
                          if (event.key === 'Enter') void handleUnlock();
                        }}
                      />
                      <button
                        className="rounded bg-blue-600 px-4 py-2 text-sm text-white hover:bg-blue-700 disabled:opacity-50"
                        disabled={!vaultPassword.trim() || vaultBusy}
                        onClick={handleUnlock}
                      >
                        {vault.fileExists ? formatMessage('解锁') : formatMessage('创建并解锁')}
                      </button>
                    </div>
                  </div>
                )}

                {vault?.fileUnlocked && (
                  <div className="flex items-center justify-between rounded-lg border border-gray-200 p-3">
                    <span className="text-xs text-gray-500">
                      {vault.keyringAvailable
                        ? formatMessage('旧加密文件已解锁，凭据已迁移至系统密钥链')
                        : formatMessage('Vault 已解锁，凭据可正常读写')}
                    </span>
                    <button
                      className="rounded border px-4 py-1.5 text-sm hover:bg-gray-50 hover:border-gray-300 disabled:opacity-50"
                      disabled={vaultBusy}
                      onClick={handleLock}
                    >
                      {formatMessage('锁定')}
                    </button>
                  </div>
                )}

                {vault?.keyringAvailable && (
                  <div className="rounded-lg border border-green-200 bg-green-50 p-3">
                    <p className="text-xs text-green-700">
                      {formatMessage('凭据已安全存储在系统密钥链中，无需手动解锁。')}
                    </p>
                  </div>
                )}

                <p className="text-xs text-gray-400 leading-relaxed">
                  {formatMessage('凭据优先存入系统密钥链；不可用时使用 Argon2id 与 AES-GCM 加密文件。主密码不会写入磁盘。')}
                </p>
              </div>
            )}

            {/* 网络 */}
            {cat === 'network' && (
              <div className="space-y-5">
                {/* 代理 */}
                <div>
                  <h3 className="mb-2 text-sm font-medium text-gray-700">{formatMessage('代理')}</h3>
                  <div className="space-y-1.5">
                    {([
                      ['system', formatMessage('使用系统代理')],
                      ['manual', formatMessage('手动配置')],
                      ['none', formatMessage('直连（不使用代理）')],
                    ] as const).map(([mode, label]) => (
                      <label key={mode} className="flex items-center gap-2 text-sm">
                        <input type="radio" checked={proxy.mode === mode} onChange={() => setProxy({ ...proxy, mode })} />
                        {label}
                      </label>
                    ))}
                  </div>
                  {proxy.mode === 'manual' && (
                    <input
                      className="mt-2 w-full rounded border border-gray-200 px-3 py-2 font-mono text-xs focus:border-blue-400 focus:outline-none"
                      placeholder={formatMessage('http://127.0.0.1:7890 或 socks5://127.0.0.1:1080')}
                      value={proxy.url ?? ''}
                      onChange={(event) => setProxy({ ...proxy, url: event.target.value })}
                    />
                  )}
                </div>

                {/* TLS */}
                <div className="border-t pt-4">
                  <h3 className="mb-2 text-sm font-medium text-gray-700">{formatMessage('TLS 证书')}</h3>
                  <div className="space-y-3">
                    {tlsFields.map(([key, label, title]) => (
                      <div key={key}>
                        <label className="mb-1 block text-xs text-gray-500">{label}</label>
                        <div className="flex gap-2">
                          <input
                            className="min-w-0 flex-1 rounded border border-gray-200 px-3 py-1.5 font-mono text-xs focus:border-blue-400 focus:outline-none"
                            placeholder={formatMessage('留空 = 不使用')}
                            value={tls[key] ?? ''}
                            onChange={(event) => setTls({ ...tls, [key]: event.target.value })}
                          />
                          <button className="rounded border px-3 py-1.5 text-xs text-gray-600 hover:bg-gray-50 hover:border-gray-300" onClick={() => void chooseCertificate(key, title)}>
                            {formatMessage('浏览…')}
                          </button>
                        </div>
                      </div>
                    ))}
                  </div>
                </div>
              </div>
            )}

            {/* 同步 */}
            {cat === 'sync' && (
              <div className="space-y-3">
                <h3 className="text-sm font-medium text-gray-700">{formatMessage('WebDAV 同步')}</h3>
                <div>
                  <label className="mb-1 block text-xs text-gray-500">{formatMessage('服务器地址')}</label>
                  <input
                    className="w-full rounded border border-gray-200 px-3 py-2 font-mono text-xs focus:border-blue-400 focus:outline-none"
                    placeholder="https://dav.example.com/remote.php/dav/files/USER/"
                    value={dav.url ?? ''}
                    onChange={(event) => setDav({ ...dav, url: event.target.value })}
                  />
                </div>
                <div className="flex gap-3">
                  <div className="flex-1">
                    <label className="mb-1 block text-xs text-gray-500">{formatMessage('用户名')}</label>
                    <input className="w-full rounded border border-gray-200 px-3 py-2 font-mono text-xs focus:border-blue-400 focus:outline-none" value={dav.username ?? ''} onChange={(event) => setDav({ ...dav, username: event.target.value })} />
                  </div>
                  <div className="flex-1">
                    <label className="mb-1 block text-xs text-gray-500">{formatMessage('密码 / 应用授权码')}</label>
                    <input
                      type="password"
                      className="w-full rounded border border-gray-200 px-3 py-2 font-mono text-xs focus:border-blue-400 focus:outline-none"
                      placeholder={dav.passwordSet ? formatMessage('已保存；留空则保持不变') : formatMessage('未设置')}
                      value={dav.password ?? ''}
                      onChange={(event) => setDav({ ...dav, password: event.target.value, clearPassword: false })}
                    />
                    {dav.passwordSet && !dav.clearPassword && (
                      <button
                        className="mt-1 text-xs text-red-600 hover:text-red-700"
                        onClick={() => setDav({ ...dav, password: '', passwordSet: false, clearPassword: true })}
                      >
                        {formatMessage('清除已保存密码')}
                      </button>
                    )}
                    {dav.clearPassword && <span className="mt-1 block text-xs text-amber-600">{formatMessage('保存后清除密码')}</span>}
                  </div>
                </div>
                <label className="flex items-center gap-2 text-xs text-gray-600">
                  <input type="checkbox" checked={dav.omitSecrets ?? false} onChange={(event) => setDav({ ...dav, omitSecrets: event.target.checked })} />
                  {formatMessage('不上传密钥变量的值')}
                </label>
                <p className="text-xs text-gray-400 leading-relaxed">{formatMessage('快照存于远端 ApiRequest/ 目录，实体级"最后写入优先"合并；顶栏手动触发同步。')}</p>
              </div>
            )}

            {/* 关于 */}
            {cat === 'about' && (
              <div className="space-y-4">
                <div className="text-center pt-4">
                  <div className="text-2xl font-bold text-gray-800">ApiRequest</div>
                  <div className="mt-1 text-xs text-gray-400">v1.0.0</div>
                </div>
                <div className="space-y-2 text-xs text-gray-500 border-t pt-4">
                  <div className="flex justify-between">
                    <span>{formatMessage('作者')}</span>
                    <span className="text-gray-700">AutumnNazi</span>
                  </div>
                  <div className="flex justify-between">
                    <span>{formatMessage('技术栈')}</span>
                    <span className="text-gray-700">Wails + React + Go</span>
                  </div>
                  <div className="flex justify-between">
                    <span>{formatMessage('引擎')}</span>
                    <span className="text-gray-700">WebView2</span>
                  </div>
                </div>
                <div className="border-t pt-4">
                  <button className="rounded border px-4 py-2 text-xs text-gray-600 hover:bg-gray-50 hover:border-gray-300" onClick={openReleasePage}>
                    {formatMessage('检查更新并打开下载页')}
                  </button>
                </div>
              </div>
            )}

            {error && <p className="mt-4 text-xs text-red-600" role="alert"><Verbatim value={error} /></p>}
            {msg && <p className="mt-4 text-xs text-green-600" role="status">{msg}</p>}
          </div>

          {/* 底部操作栏 */}
          <div className="flex justify-end gap-2 border-t px-5 py-3">
            <button className="rounded border px-4 py-2 text-sm text-gray-600 hover:bg-gray-50 hover:border-gray-300" onClick={onClose}>
              {formatMessage('取消')}
            </button>
            <button className="rounded bg-blue-600 px-4 py-2 text-sm text-white hover:bg-blue-700" onClick={save}>
              {formatMessage('保存')}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
