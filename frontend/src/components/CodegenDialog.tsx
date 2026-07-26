// 代码生成弹窗：目标语言选择 + 片段预览 + 复制
import { useEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { codegenTargets, generateCode, type HttpRequest } from '../ipc';

interface Props {
  request: HttpRequest;
  onClose(): void;
}

export default function CodegenDialog({ request, onClose }: Props) {
  const [target, setTarget] = useState('curl');
  const [code, setCode] = useState('');
  const [copied, setCopied] = useState(false);

  const { data: targets = [] } = useQuery({
    queryKey: ['codegen-targets'],
    queryFn: codegenTargets,
    staleTime: Infinity,
  });

  useEffect(() => {
    let alive = true;
    generateCode(target, request)
      .then((c) => alive && setCode(c))
      .catch((e) => alive && setCode(`// 生成失败: ${e.detail ?? e}`));
    return () => {
      alive = false;
    };
  }, [target, request]);

  const copy = async () => {
    await navigator.clipboard.writeText(code);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };

  return (
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50" onClick={onClose}>
      <div
        className="bg-white rounded-lg shadow-xl w-[640px] h-[480px] flex flex-col"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center gap-3 px-4 py-3 border-b">
          <h2 className="font-semibold text-sm">生成代码</h2>
          <select
            className="border rounded px-2 py-1 text-xs"
            value={target}
            onChange={(e) => setTarget(e.target.value)}
          >
            {targets.map((t) => (
              <option key={t.id} value={t.id}>
                {t.name}
              </option>
            ))}
          </select>
          <button
            className="ml-auto text-xs border rounded px-3 py-1 hover:bg-gray-50"
            onClick={copy}
          >
            {copied ? '已复制 ✓' : '复制'}
          </button>
          <button className="text-gray-400 hover:text-gray-700" onClick={onClose}>
            ×
          </button>
        </div>
        <pre className="flex-1 overflow-auto p-4 text-xs font-mono bg-gray-50 whitespace-pre-wrap break-all rounded-b-lg">
          {code}
        </pre>
      </div>
    </div>
  );
}
