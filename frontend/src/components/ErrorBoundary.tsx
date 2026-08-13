import React from 'react';
import { Verbatim, formatMessage } from '../i18n/locale';

interface State { err: Error | null }

// 全局 React 错误边界：任一组件抛错时不白屏，显示降级界面 + 重置按钮。
export class ErrorBoundary extends React.Component<{ children: React.ReactNode }, State> {
  state: State = { err: null };

  static getDerivedStateFromError(err: Error): State {
    return { err };
  }

  componentDidCatch(err: Error, info: React.ErrorInfo) {
    // 输出到 console 便于排查；后续可加 IPC 兜底上报
    // eslint-disable-next-line no-console
    console.error('[ErrorBoundary] uncaught render error:', err, info.componentStack);
  }

  reset = () => this.setState({ err: null });

  render() {
    if (this.state.err) {
      return (
        <div className="h-screen w-screen flex flex-col items-center justify-center bg-slate-50 dark:bg-slate-900 text-slate-800 dark:text-slate-100 p-6">
          <div className="max-w-lg text-center">
            <div className="text-3xl mb-3">⚠️</div>
            <div className="text-lg font-semibold mb-2">{formatMessage('界面渲染出现未捕获错误')}</div>
            <pre className="mt-3 px-4 py-3 bg-slate-100 dark:bg-slate-800 rounded text-xs text-left overflow-auto max-h-48 whitespace-pre-wrap break-words">
              <Verbatim value={this.state.err.message} />
            </pre>
            <div className="mt-5 flex gap-3 justify-center">
              <button
                onClick={this.reset}
                className="px-4 py-2 rounded bg-slate-700 hover:bg-slate-600 text-white text-sm"
              >
                {formatMessage('重试')}
              </button>
              <button
                onClick={() => location.reload()}
                className="px-4 py-2 rounded bg-slate-200 dark:bg-slate-700 hover:bg-slate-300 text-sm"
              >
                {formatMessage('刷新页面')}
              </button>
            </div>
          </div>
        </div>
      );
    }
    return this.props.children;
  }
}
