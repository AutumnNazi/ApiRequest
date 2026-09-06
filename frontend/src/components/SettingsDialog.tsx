// 应用设置：左侧分类导航 + 右侧内容面板
import { useEffect, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import {
  getProxySettings,
  getNetworkStatus,
  refreshSystemProxy,
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
  type NetworkStatus,
  type TLSSettings,
  type SyncDavConfig,
  type VaultStatus,
  type RemoteWorkspaceInfo,
  type UpdateCheckResult,
  listRemoteWorkspaces,
  importRemoteWorkspace,
  checkForUpdates,
  getRawSetting,
  setRawSetting,
} from '../ipc';
import { eventCombo, formatCombo, parseHotkeySettings, detectPlatform, isSafeCombo, type HotkeyMap } from '../utils/hotkeys';
import { useLocale, Verbatim, formatMessage, type Locale } from '../i18n/locale';
import { useDialog } from './DialogProvider';
import ModalFrame from './ModalFrame';
import { useLatestTimeout } from '../hooks/useLatestTimeout';

interface Props {
  onClose(): void;
}

// 标签只存原始文案 key，渲染期再 formatMessage：
// 模块级常量会在 import 时固化翻译，切换语言后不再更新
const tlsFields: Array<[keyof TLSSettings, string, string]> = [
  ['caCertPath', '自定义 CA 证书', '选择 CA 证书'],
  ['clientCertPath', '客户端证书 (mTLS)', '选择客户端证书'],
  ['clientKeyPath', '客户端私钥', '选择客户端私钥'],
];

type Category = 'general' | 'security' | 'network' | 'hotkeys' | 'sync' | 'about';

const categories: Array<[Category, string]> = [
  ['general', '通用'],
  ['security', '安全'],
  ['network', '网络'],
  ['hotkeys', '快捷键'],
  ['sync', '同步'],
  ['about', '关于'],
];

export default function SettingsDialog({ onClose }: Props) {
  const qc = useQueryClient();
  const dialog = useDialog();
  const locale = useLocale((state) => state.locale);
  const setLocale = useLocale((state) => state.setLocale);
  const [cat, setCat] = useState<Category>('general');
  const [proxy, setProxy] = useState<ProxySettings>({ mode: 'system' });
  const [network, setNetwork] = useState<NetworkStatus | null>(null);
  const [tls, setTls] = useState<TLSSettings>({});
  const [dav, setDav] = useState<Partial<SyncDavConfig>>({});
  const [remoteWs, setRemoteWs] = useState<RemoteWorkspaceInfo[] | null>(null);
  const [remoteBusy, setRemoteBusy] = useState(false);
  const [remoteError, setRemoteError] = useState('');
  const [updateResult, setUpdateResult] = useState<UpdateCheckResult | null>(null);
  const [updateError, setUpdateError] = useState('');
  const [checking, setChecking] = useState(false);
  const hotkeysQuery = useQuery({ queryKey: ['hotkeys'], queryFn: () => getRawSetting('hotkeys') });
  const [hotkeyOverride, setHotkeyOverride] = useState<Partial<HotkeyMap>>({});
  const hotkeys: HotkeyMap = { ...parseHotkeySettings(hotkeysQuery.data), ...hotkeyOverride };
  const [capturing, setCapturing] = useState<string | null>(null);
  const [hotkeyHint, setHotkeyHint] = useState('');
  const [vault, setVault] = useState<VaultStatus | null>(null);
  const [vaultPassword, setVaultPassword] = useState('');
  const [vaultBusy, setVaultBusy] = useState(false);
  const [msg, setMsg] = useState('');
  const clearMsg = useLatestTimeout();
  const [error, setError] = useState('');

  useEffect(() => {
    Promise.all([getProxySettings(), getTLSSettings(), getSyncConfig(), getVaultStatus(), getNetworkStatus()])
      .then(([nextProxy, nextTls, nextDav, nextVault, nextNetwork]) => {
        setProxy(nextProxy);
        setTls(nextTls);
        setDav(nextDav);
        setVault(nextVault);
        setNetwork(nextNetwork);
      })
      .catch((cause) => setError(toAppError(cause).detail));
  }, []);

  const save = async () => {
    setError('');
    setMsg('');
    try {
      await setProxySettings(proxy);
      await setTLSSettings(tls);
      await setSyncConfig(dav);
      setProxy(await getProxySettings());
      setNetwork(await getNetworkStatus());
      setDav(await getSyncConfig());
      await qc.invalidateQueries({ queryKey: ['syncConfig'] });
      setVault(await getVaultStatus());
      setMsg(formatMessage('已保存并生效'));
      clearMsg(() => setMsg(''), 1500);
    } catch (cause) {
      setError(toAppError(cause).detail);
    }
  };

  const detectSystemProxy = async () => {
    setError('');
    try {
      setNetwork(await refreshSystemProxy());
      setMsg(formatMessage('系统代理已重新检测'));
      clearMsg(() => setMsg(''), 1500);
    } catch (cause) {
      setError(toAppError(cause).detail);
    }
  };

  const chooseCertificate = async (key: keyof TLSSettings, title: string) => {
    try {
      const path = await openNativeFile(formatMessage(title));
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
    <ModalFrame
      onClose={onClose}
      titleId="settings-title"
      className="flex h-[520px] w-[680px] max-w-full rounded-lg bg-white shadow-xl overflow-hidden"
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
              {formatMessage(label)}
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
                    <div className="mt-2 space-y-2">
                      <input
                        className="w-full rounded border border-gray-200 px-3 py-2 font-mono text-xs focus:border-blue-400 focus:outline-none"
                        placeholder={formatMessage('http://127.0.0.1:7890 或 socks5://127.0.0.1:1080')}
                        value={proxy.url ?? ''}
                        onChange={(event) => setProxy({ ...proxy, url: event.target.value })}
                      />
                      <div className="flex gap-2">
                        <input
                          className="min-w-0 flex-1 rounded border border-gray-200 px-3 py-1.5 text-xs focus:border-blue-400 focus:outline-none"
                          placeholder={formatMessage('代理用户名（可选）')}
                          value={proxy.username ?? ''}
                          onChange={(event) => setProxy({ ...proxy, username: event.target.value })}
                        />
                        <input
                          type="password"
                          className="min-w-0 flex-1 rounded border border-gray-200 px-3 py-1.5 text-xs focus:border-blue-400 focus:outline-none"
                          placeholder={proxy.passwordSet ? formatMessage('密码已保存；留空则保持不变') : formatMessage('代理密码（可选）')}
                          value={proxy.password ?? ''}
                          onChange={(event) => setProxy({ ...proxy, password: event.target.value, clearPassword: false })}
                        />
                      </div>
                      {proxy.passwordSet && !proxy.clearPassword && (
                        <button
                          className="text-xs text-red-600 hover:text-red-700"
                          onClick={() => setProxy({ ...proxy, password: '', passwordSet: false, clearPassword: true })}
                        >
                          {formatMessage('清除已保存代理密码')}
                        </button>
                      )}
                    </div>
                  )}

                  {proxy.mode === 'system' && (
                    <div className="mt-3 rounded border border-gray-200 bg-gray-50 p-3 text-xs text-gray-600">
                      <div className="flex items-center justify-between gap-3">
                        <span>{formatMessage('当前来源')}: {network?.proxySource || formatMessage('检测中…')}</span>
                        <button className="rounded border px-2.5 py-1 text-xs hover:bg-white hover:border-gray-300" onClick={() => void detectSystemProxy()}>
                          {formatMessage('重新检测')}
                        </button>
                      </div>
                      {network?.proxyWarning && <p className="mt-2 text-amber-600">{network.proxyWarning}</p>}
                    </div>
                  )}
                </div>

                {/* TLS */}
                <div className="border-t pt-4">
                  <h3 className="mb-2 text-sm font-medium text-gray-700">{formatMessage('TLS 证书')}</h3>
                  <div className="space-y-3">
                    {tlsFields.map(([key, label, title]) => (
                      <div key={key}>
                        <label className="mb-1 block text-xs text-gray-500">{formatMessage(label)}</label>
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
                  {network?.tlsWarning && <p className="mt-2 text-xs text-amber-600">{network.tlsWarning}</p>}
                  <p className="mt-2 text-xs text-gray-400">
                    {network?.tlsActive ? formatMessage('自定义 TLS 配置已生效') : formatMessage('当前使用系统默认 TLS')}
                  </p>
                </div>
              </div>
            )}

            {/* 快捷键 */}
            {cat === 'hotkeys' && (
              <div className="space-y-3">
                {(
                  [
                    ['send', '发送请求'],
                    ['save', '保存'],
                    ['newTab', '新建标签'],
                    ['closeTab', '关闭标签'],
                    ['env', '切换环境'],
                    ['palette', '命令面板'],
                  ] as Array<[keyof HotkeyMap, string]>
                ).map(([action, label]) => (
                  <div key={action} className="flex items-center gap-3 text-xs">
                    <span className="w-24 text-gray-600">{formatMessage(label)}</span>
                    <input
                      readOnly
                      data-hotkey={action} data-testid={`hotkey-${action}`}
                      className="border rounded px-2 py-1 font-mono text-xs w-32 cursor-pointer bg-white"
                      value={capturing === action
                        ? formatMessage('按下新组合键…')
                        : formatCombo(hotkeys[action], detectPlatform())}
                      onKeyDown={(event) => {
                        event.preventDefault();
                        const combo = eventCombo(event.nativeEvent);
                        if (!combo) return;
                        if (!isSafeCombo(combo)) {
                          setHotkeyHint(formatMessage('需包含 Ctrl/Cmd，或使用 F1-F12 键'));
                          return;
                        }
                        setHotkeyHint('');
                        const next = { ...hotkeys, [action]: combo } as HotkeyMap;
                        setHotkeyOverride(next);
                        void setRawSetting('hotkeys', JSON.stringify(next)).then(() => {
                          qc.invalidateQueries({ queryKey: ['hotkeys'] });
                        });
                        setCapturing(null);
                      }}
                      onFocus={() => setCapturing(action)}
                      onBlur={() => {
                        setCapturing(null);
                        setHotkeyHint('');
                      }}
                    />
                    <span className="text-gray-400">
                      {capturing === action ? formatMessage('Esc 取消') : formatMessage('点击后按下新组合键')}
                    </span>
                  </div>
                ))}
                {hotkeyHint && <p className="text-xs text-amber-600">{hotkeyHint}</p>}
                <p className="text-xs text-gray-400 leading-relaxed">
                  {formatMessage('组合需包含 Ctrl/Cmd（或使用 F1-F12），避免与输入冲突；保存即时生效。')}
                </p>
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
                <div className="flex items-center gap-3 text-xs">
                  <label className="text-gray-600">{formatMessage('自动同步间隔（分钟，0 = 关闭）')}</label>
                  <input
                    type="number"
                    min={0}
                    max={1440}
                    className="border rounded px-2 py-1 w-24"
                    value={dav.intervalMinutes ?? 0}
                    onChange={(event) => setDav({ ...dav, intervalMinutes: Number(event.target.value) || 0 })}
                  />
                </div>
                <p className="text-xs text-gray-400 leading-relaxed">{formatMessage('快照存于远端 ApiRequest/ 目录，字段级三路合并（本地胜出并提示冲突）；顶栏手动触发同步，设置间隔后自动执行，跨设备经 If-Match 条件写入互斥。')}</p>

                <div className="border rounded p-3 space-y-2">
                  <div className="flex items-center gap-2">
                    <button
                      className="border rounded px-2 py-1 text-xs text-gray-700 hover:bg-gray-50 disabled:cursor-not-allowed disabled:opacity-50"
                      disabled={remoteBusy || !(dav.url ?? '').trim()}
                      onClick={() => {
                        setRemoteBusy(true);
                        setRemoteError('');
                        listRemoteWorkspaces()
                          .then((items) => setRemoteWs(items))
                          .catch((cause) => setRemoteError(toAppError(cause).detail))
                          .finally(() => setRemoteBusy(false));
                      }}
                    >
                      {remoteBusy ? formatMessage('发现中…') : formatMessage('从远端导入工作区')}
                    </button>
                    <span className="text-gray-400">{formatMessage('新设备接入同一快照：导入后即可双向同步')}</span>
                  </div>
                  {remoteError && <p className="text-xs text-red-600"><Verbatim value={remoteError} /></p>}
                  {remoteWs && remoteWs.length === 0 && (
                    <p className="text-xs text-gray-400">{formatMessage('远端未发现任何工作区快照')}</p>
                  )}
                  {(remoteWs ?? []).map((item) => (
                    <div key={item.workspaceId} className="flex items-center gap-2 text-xs">
                      <span className="flex-1 min-w-0 truncate">
                        <Verbatim value={item.name || item.workspaceId} />
                        <span className="text-gray-400"> · {item.workspaceId}</span>
                      </span>
                      <button
                        className="border rounded px-2 py-0.5 text-blue-600 hover:bg-blue-50"
                        onClick={() => {
                          setRemoteBusy(true);
                          setRemoteError('');
                          importRemoteWorkspace(item.workspaceId)
                            .then(() => {
                              setRemoteWs(null);
                              void dialog.alert(formatMessage('已导入。左侧工作区列表中切换即可使用。'), { title: formatMessage('导入成功') });
                              qc.invalidateQueries({ queryKey: ['workspaces'] });
                            })
                            .catch((cause) => setRemoteError(toAppError(cause).detail))
                            .finally(() => setRemoteBusy(false));
                        }}
                      >
                        {formatMessage('导入')}
                      </button>
                    </div>
                  ))}
                </div>
              </div>
            )}

            {/* 关于 */}
            {cat === 'about' && (
              <div className="space-y-4">
                <div className="text-center pt-4">
                  <div className="text-2xl font-bold text-gray-800">ApiRequest</div>
                  <div className="mt-1 text-xs text-gray-400">v1.0.0</div>
                  <button
                    className="mt-3 border rounded px-3 py-1 text-xs text-gray-600 hover:bg-gray-50 disabled:opacity-50"
                    disabled={checking}
                    onClick={() => {
                      setChecking(true);
                      setUpdateError('');
                      checkForUpdates()
                        .then((r) => setUpdateResult(r))
                        .catch((cause) => setUpdateError(toAppError(cause).detail))
                        .finally(() => setChecking(false));
                    }}
                  >
                    {checking ? formatMessage('检查中…') : formatMessage('检查更新')}
                  </button>
                  {updateError && (
                    <p className="mt-2 text-xs text-red-600"><Verbatim value={updateError} /></p>
                  )}
                  {updateResult && (
                    <div
                      className={`mt-3 text-left text-xs border rounded p-3 space-y-1 ${
                        updateResult.status === 'verification-failed'
                          ? 'border-red-200 bg-red-50 text-red-700'
                          : 'border-gray-200 bg-gray-50 text-gray-600'
                      }`}
                    >
                      {updateResult.status === 'available' && (
                        <p className="font-medium text-green-700">
                          {formatMessage('发现新版本 {version}（已通过签名验证）', { version: updateResult.latestVersion ?? '' })}
                        </p>
                      )}
                      {updateResult.status === 'up-to-date' && (
                        <p>{formatMessage('已是最新版本')}</p>
                      )}
                      {updateResult.status === 'manual-required' && (
                        <p className="font-medium text-amber-700">
                          {formatMessage('发现新版本 {version}：当前版本低于最低自动升级线，请手动安装', { version: updateResult.latestVersion ?? '' })}
                        </p>
                      )}
                      {updateResult.status === 'verification-failed' && (
                        <p className="font-medium">{formatMessage('更新清单签名验证失败，已拒绝本次内容')}</p>
                      )}
                      {updateResult.detail && <p><Verbatim value={updateResult.detail} /></p>}
                      {(updateResult.downloadUrl || updateResult.notesUrl) && (
                        <button
                          className="text-blue-600 hover:underline"
                          onClick={() => void openReleasePage()}
                        >
                          {formatMessage('打开下载页')}
                        </button>
                      )}
                    </div>
                  )}
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
    </ModalFrame>
  );
}
