// 最近地址建议条：显示在目标输入框下方，点击即填入。供 WS/gRPC/GraphQL 面板复用。
import { formatMessage, Verbatim } from '../i18n/locale';

interface Props {
  recents: string[];
  current: string;
  onPick(value: string): void;
}

export default function RecentTargets({ recents, current, onPick }: Props) {
  const shown = recents.filter((r) => r !== current).slice(0, 5);
  if (shown.length === 0) return null;
  return (
    <div className="flex flex-wrap items-center gap-1 px-4 pb-2">
      <span className="text-[10px] text-gray-400">{formatMessage('最近:')}</span>
      {shown.map((r) => (
        <button
          key={r}
          type="button"
          className="max-w-56 truncate rounded border border-gray-200 px-1.5 py-0.5 text-[11px] font-mono text-gray-600 hover:border-blue-300 hover:text-blue-600"
          onClick={() => onPick(r)}
        >
          <Verbatim value={r} />
        </button>
      ))}
    </div>
  );
}
