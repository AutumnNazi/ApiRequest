import { formatMessage, Verbatim } from '../i18n/locale';

interface Props {
  message: string;
  detail: string;
  onRetry(): void;
  fullScreen?: boolean;
}

export default function QueryErrorState({ message, detail, onRetry, fullScreen = false }: Props) {
  return (
    <div
      className={
        fullScreen
          ? 'flex h-screen flex-col items-center justify-center gap-2 px-6 text-center text-sm'
          : 'flex min-w-0 items-center gap-2 text-xs'
      }
      role="alert"
    >
      <span className="max-w-full truncate text-red-600" title={detail}>
        <Verbatim value={formatMessage('{message}：{detail}', { message, detail })} />
      </span>
      <button
        type="button"
        className="shrink-0 rounded border px-2 py-1 text-gray-600 hover:bg-gray-100"
        onClick={onRetry}
      >
        {formatMessage('重试')}
      </button>
    </div>
  );
}
