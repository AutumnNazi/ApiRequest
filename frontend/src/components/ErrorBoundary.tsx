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

// 对话框级错误边界：懒加载面板（设置/gRPC/GraphQL/Cookie 等）渲染抛错时，
// 只把错误圈在这个浮层里并允许关闭，标签页与未保存草稿不受影响——
// 没有它时任一面板抛错会冒泡到根边界，用户丢掉全部打开的请求。
export class DialogErrorBoundary extends React.Component<
  { children: React.ReactNode; onClose(): void },
  State
> {
  state: State = { err: null };

  static getDerivedStateFromError(err: Error): State {
    return { err };
  }

  componentDidCatch(err: Error, info: React.ErrorInfo) {
    console.error('[DialogErrorBoundary] dialog render error:', err, info.componentStack);
  }

  // 先清本地错误态再通知宿主卸载：宿主若保留挂载（如复用同一实例），重开仍是干净的
  close = () => {
    this.setState({ err: null });
    this.props.onClose();
  };

  render() {
    if (this.state.err) {
      return (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-6">
          <div
            role="alertdialog"
            aria-modal="true"
            className="w-full max-w-md rounded-lg bg-white dark:bg-slate-800 p-5 shadow-xl text-slate-800 dark:text-slate-100"
          >
            <div className="text-sm font-semibold mb-2">{formatMessage('该面板加载失败')}</div>
            <pre className="px-3 py-2 bg-slate-100 dark:bg-slate-900 rounded text-xs overflow-auto max-h-40 whitespace-pre-wrap break-words">
              <Verbatim value={this.state.err.message} />
            </pre>
            <div className="mt-4 flex justify-end">
              <button
                onClick={this.close}
                className="px-3 py-1.5 rounded border text-xs hover:bg-gray-50 dark:hover:bg-slate-700 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
              >
                {formatMessage('关闭')}
              </button>
            </div>
          </div>
        </div>
      );
    }
    return this.props.children;
  }
}
