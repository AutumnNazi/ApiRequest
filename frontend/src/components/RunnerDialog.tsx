// Collection Runner 弹窗：配置 → 运行进度 → 报告
import { useEffect, useRef, useState } from 'react';
import {
  runCollection,
  cancelRun,
  exportReport,
  openNativeFile,
  readNativeTextFile,
  onRunnerProgress,
  toAppError,
  type RunnerReport,
  type RunnerProgress,
} from '../ipc';
import { formatMessage, Verbatim } from '../i18n/locale';
import { useDialog } from './DialogProvider';

interface Props {
  workspaceId: string;
  collectionId: string;
  collectionName: string;
  onClose(): void;
}

export default function RunnerDialog({ workspaceId, collectionId, collectionName, onClose }: Props) {
  const dialog = useDialog();
  const [iterations, setIterations] = useState(1);
  const [dataFile, setDataFile] = useState('');
  const [dataFilePath, setDataFilePath] = useState('');
  const [dataFormat, setDataFormat] = useState<'csv' | 'json'>('csv');
  const [stopOnError, setStopOnError] = useState(false);
  const [running, setRunning] = useState(false);
  const [progress, setProgress] = useState<RunnerProgress | null>(null);
  const [report, setReport] = useState<RunnerReport | null>(null);
  const [error, setError] = useState('');
  const [exporting, setExporting] = useState(false);
  const runIdRef = useRef('');

  useEffect(() => {
    const unsub = onRunnerProgress((p) => {
      if (p.runId === runIdRef.current) setProgress(p);
    });
    return unsub;
  }, []);

  useEffect(() => () => {
    if (runIdRef.current) void cancelRun(runIdRef.current);
  }, []);

  const start = async () => {
    setError('');
    setReport(null);
    setProgress(null);
    setRunning(true);
    runIdRef.current = `run-${Date.now()}`;
    try {
      const r = await runCollection(runIdRef.current, workspaceId, collectionId, {
        iterations,
        dataFile: dataFile || undefined,
        dataFormat: dataFile ? dataFormat : undefined,
        stopOnError,
      });
      setReport(r);
    } catch (e) {
      setError(toAppError(e).detail);
    } finally {
      runIdRef.current = '';
      setRunning(false);
    }
  };

  const requestClose = async () => {
    if (running && runIdRef.current) {
      const confirmed = await dialog.confirm(formatMessage('Runner 正在运行，是否取消并关闭？'), {
        title: formatMessage('取消 Runner'),
        confirmLabel: formatMessage('取消并关闭'),
      });
      if (!confirmed) return;
      try {
        await cancelRun(runIdRef.current);
        runIdRef.current = '';
      } catch (cause) {
        setError(toAppError(cause).detail);
        return;
      }
    }
    onClose();
  };

  const pickFile = async () => {
    setError('');
    try {
      const path = await openNativeFile('选择 Runner 数据文件');
      if (!path) return;
      setDataFile(await readNativeTextFile(path));
      setDataFilePath(path);
      setDataFormat(path.toLowerCase().endsWith('.json') ? 'json' : 'csv');
    } catch (cause) {
      setError(toAppError(cause).detail);
    }
  };

  const handleExport = async () => {
    if (!report || exporting) return;
    setError('');
    setExporting(true);
    try {
      const text = await exportReport(report.runId);
      await navigator.clipboard.writeText(text);
      void dialog.alert(formatMessage('报告 JSON 已复制到剪贴板'));
    } catch (cause) {
      setError(toAppError(cause).detail);
    } finally {
      setExporting(false);
    }
  };

  return (
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50" onClick={() => void requestClose()}>
      <div
        className="bg-white rounded-lg shadow-xl w-[720px] max-h-[85vh] flex flex-col"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center px-4 py-3 border-b">
          <h2 className="font-semibold text-sm">Runner · <Verbatim value={collectionName} /></h2>
          <button className="ml-auto text-gray-400 hover:text-gray-700" onClick={() => void requestClose()} aria-label={formatMessage('关闭 Runner')}>
            ×
          </button>
        </div>

        <div className="flex-1 overflow-auto p-4 space-y-4">
          {/* 配置区 */}
          {!running && !report && (
            <div className="space-y-3 text-sm">
              <div className="flex items-center gap-3">
                <label className="text-gray-600 w-24">{formatMessage('迭代次数')}</label>
                <input
                  type="number"
                  min={1}
                  max={1000}
                  className="border rounded px-2 py-1 w-24"
                  value={iterations}
                  onChange={(e) => setIterations(Number(e.target.value) || 1)}
                  disabled={!!dataFile}
                />
                {dataFile && <span className="text-xs text-gray-400">{formatMessage('由数据文件行数决定')}</span>}
              </div>
              <div className="flex items-center gap-3">
                <label className="text-gray-600 w-24">{formatMessage('数据文件')}</label>
                <button className="border rounded px-2 py-1 text-xs hover:bg-gray-50" onClick={() => void pickFile()}>
                  {formatMessage('选择 CSV / JSON…')}
                </button>
                {dataFilePath && <span className="min-w-0 truncate text-xs text-gray-400" title={dataFilePath} data-i18n-verbatim><Verbatim value={dataFilePath} /></span>}
                {dataFile && (
                  <button className="text-xs text-red-500" onClick={() => { setDataFile(''); setDataFilePath(''); }}>
                    {formatMessage('清除')}
                  </button>
                )}
              </div>
              <label className="flex items-center gap-2 text-gray-600">
                <input
                  type="checkbox"
                  checked={stopOnError}
                  onChange={(e) => setStopOnError(e.target.checked)}
                />
                {formatMessage('失败时停止')}
              </label>
            </div>
          )}

          {/* 进度区 */}
          {running && (
            <div className="space-y-2">
              <div className="h-2 bg-gray-100 rounded overflow-hidden">
                <div
                  className="h-2 bg-blue-500 transition-all"
                  style={{ width: `${progress ? (progress.done / progress.total) * 100 : 0}%` }}
                />
              </div>
              <div className="text-sm text-gray-600">
                {progress
                  ? <Verbatim value={formatMessage('{done}/{total} · 第 {iteration} 轮 · {requestName}', {
                      done: progress.done,
                      total: progress.total,
                      iteration: progress.iteration,
                      requestName: progress.requestName,
                    })} />
                  : formatMessage('启动中…')}
              </div>
            </div>
          )}

          {error && (
            <div className="border border-red-200 bg-red-50 rounded p-2 text-xs text-red-600">
              <Verbatim value={error} />
            </div>
          )}

          {/* 报告区 */}
          {report && (
            <div className="space-y-3">
              <div className="flex gap-4 text-sm">
                <span className="text-green-600 font-medium">✓ {report.passed} {formatMessage('通过')}</span>
                <span className={report.failed > 0 ? 'text-red-600 font-medium' : 'text-gray-400'}>
                  ✗ {report.failed} {formatMessage('失败')}
                </span>
                {report.skipped > 0 && <span className="text-gray-400">− {report.skipped} {formatMessage('跳过')}</span>}
                <span className="text-gray-400 ml-auto">
                  {(report.durationMs / 1000).toFixed(1)}s{report.canceled ? formatMessage('（已取消）') : ''}
                </span>
              </div>
              <table className="w-full text-xs border rounded">
                <thead className="bg-gray-50 text-gray-500">
                  <tr className="text-left">
                    <th className="p-2 font-normal w-10">{formatMessage('轮')}</th>
                    <th className="p-2 font-normal">{formatMessage('请求')}</th>
                    <th className="p-2 font-normal w-14">{formatMessage('状态码')}</th>
                    <th className="p-2 font-normal w-16">{formatMessage('耗时')}</th>
                    <th className="p-2 font-normal">{formatMessage('测试')}</th>
                  </tr>
                </thead>
                <tbody>
                  {report.results.map((r, i) => (
                    <tr key={i} className={`border-t ${r.failed ? 'bg-red-50' : ''}`}>
                      <td className="p-2 text-gray-400">{r.iteration}</td>
                      <td className="p-2"><Verbatim value={r.requestName} /></td>
                      <td className="p-2">{r.status || '—'}</td>
                      <td className="p-2 text-gray-500">{r.durationMs}ms</td>
                      <td className="p-2">
                        {r.error ? (
                          <span className="text-red-600"><Verbatim value={r.error} /></span>
                        ) : (
                          (r.testResults ?? []).map((t, ti) => (
                            <span
                              key={ti}
                              className={`mr-2 ${t.pass ? 'text-green-600' : 'text-red-600'}`}
                              title={t.error}
                              data-i18n-verbatim
                            >
                              {t.pass ? '✓' : '✗'} <Verbatim value={t.name} />
                            </span>
                          ))
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>

        <div className="flex justify-end gap-2 px-4 py-3 border-t">
          {running ? (
            <button
              className="border border-red-200 text-red-500 rounded px-4 py-1.5 text-sm hover:bg-red-50"
              onClick={() => {
                void cancelRun(runIdRef.current).catch((cause) => {
                  setError(toAppError(cause).detail);
                });
              }}
            >
              {formatMessage('取消')}
            </button>
          ) : (
            <>
              {report && (
                <button
                  className="border rounded px-4 py-1.5 text-sm hover:bg-gray-50 disabled:cursor-not-allowed disabled:opacity-50"
                  onClick={() => void handleExport()}
                  disabled={exporting}
                >
                  {exporting ? formatMessage('导出中…') : formatMessage('导出报告')}
                </button>
              )}
              <button
                className="bg-blue-600 text-white rounded px-4 py-1.5 text-sm hover:bg-blue-700"
                onClick={start}
              >
                {report ? formatMessage('重新运行') : formatMessage('开始运行')}
              </button>
            </>
          )}
        </div>
      </div>
    </div>
  );
}
