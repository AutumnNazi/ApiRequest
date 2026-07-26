// 导入弹窗：粘贴 Postman JSON / cURL → 预览 → 确认落库
import { useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { importPreview, importCommit, toAppError, type ImportResult } from '../ipc';

interface Props {
  workspaceId: string;
  onClose(): void;
}

export default function ImportDialog({ workspaceId, onClose }: Props) {
  const qc = useQueryClient();
  const [payload, setPayload] = useState('');
  const [preview, setPreview] = useState<ImportResult | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  const doPreview = async () => {
    setError('');
    setBusy(true);
    try {
      setPreview(await importPreview('auto', payload));
    } catch (e) {
      setPreview(null);
      setError(toAppError(e).detail);
    } finally {
      setBusy(false);
    }
  };

  const doCommit = async () => {
    if (!preview) return;
    setBusy(true);
    try {
      await importCommit(workspaceId, preview);
      qc.invalidateQueries({ queryKey: ['nodes', workspaceId] });
      onClose();
    } catch (e) {
      setError(toAppError(e).detail);
    } finally {
      setBusy(false);
    }
  };

  const requestCount = preview?.children.filter((n) => n.kind === 'request').length ?? 0;

  return (
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50" onClick={onClose}>
      <div
        className="bg-white rounded-lg shadow-xl w-[640px] max-h-[80vh] flex flex-col"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center px-4 py-3 border-b">
          <h2 className="font-semibold text-sm">导入</h2>
          <span className="ml-2 text-xs text-gray-400">支持 Postman v2.1 JSON / cURL 命令</span>
          <button className="ml-auto text-gray-400 hover:text-gray-700" onClick={onClose}>
            ×
          </button>
        </div>
        <div className="flex-1 overflow-auto p-4 space-y-3">
          <textarea
            className="w-full h-40 border rounded p-2 text-xs font-mono outline-none focus:border-blue-400"
            placeholder={'粘贴 Postman 集合 JSON，或 curl 命令…'}
            value={payload}
            onChange={(e) => {
              setPayload(e.target.value);
              setPreview(null);
            }}
          />
          {error && (
            <div className="border border-red-200 bg-red-50 rounded p-2 text-xs text-red-600">
              {error}
            </div>
          )}
          {preview && (
            <div className="border rounded p-3 text-sm space-y-1">
              <div>
                📁 <span className="font-medium">{preview.collection.name}</span>
                <span className="text-gray-400 text-xs ml-2">{requestCount} 个请求</span>
              </div>
              <ul className="text-xs text-gray-600 max-h-40 overflow-auto">
                {preview.children.map((n) => (
                  <li key={n.id} className="truncate">
                    {n.kind === 'folder' ? '📂' : '·'} {n.name}
                  </li>
                ))}
              </ul>
              {(preview.warnings ?? []).length > 0 && (
                <div className="text-xs text-yellow-600">
                  {preview.warnings!.map((w, i) => (
                    <div key={i}>⚠ {w}</div>
                  ))}
                </div>
              )}
            </div>
          )}
        </div>
        <div className="flex justify-end gap-2 px-4 py-3 border-t">
          <button
            className="border rounded px-4 py-1.5 text-sm hover:bg-gray-50 disabled:opacity-50"
            disabled={!payload.trim() || busy}
            onClick={doPreview}
          >
            预览
          </button>
          <button
            className="bg-blue-600 text-white rounded px-4 py-1.5 text-sm hover:bg-blue-700 disabled:opacity-50"
            disabled={!preview || busy}
            onClick={doCommit}
          >
            确认导入
          </button>
        </div>
      </div>
    </div>
  );
}
