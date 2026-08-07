import React from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import './style.css';
import './theme/theme.css';
import './theme/store'; // 模块加载即恢复持久化主题
import App from './App';
import { ErrorBoundary } from './components/ErrorBoundary';
import { DialogProvider } from './components/DialogProvider';

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // 本地 IPC 无网络抖动，失败即真错误，不重试
      retry: false,
      refetchOnWindowFocus: false,
      // mutation 会主动 invalidate；有限 staleTime 也能兜住遗漏的跨面板更新。
      staleTime: 30_000,
    },
  },
});

const container = document.getElementById('root');
const root = createRoot(container!);

root.render(
  <React.StrictMode>
    <ErrorBoundary>
      <QueryClientProvider client={queryClient}>
        <DialogProvider>
          <App />
        </DialogProvider>
      </QueryClientProvider>
    </ErrorBoundary>
  </React.StrictMode>,
);
