import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { DialogErrorBoundary, ErrorBoundary } from './ErrorBoundary';

// React 会把边界捕获的错误再 console.error 一次，测试里静音以免污染输出
let consoleSpy: ReturnType<typeof vi.spyOn>;
beforeEach(() => {
  consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => undefined);
});
afterEach(() => consoleSpy.mockRestore());

function Boom(): JSX.Element {
  throw new Error('dialog exploded');
}

describe('DialogErrorBoundary', () => {
  // 核心不变量：对话框炸了只关掉对话框，主界面（标签页/未保存草稿）必须存活
  it('隔离对话框渲染错误，主界面继续存活', () => {
    render(
      <div>
        <div>主界面存活标记</div>
        <DialogErrorBoundary onClose={vi.fn()}>
          <Boom />
        </DialogErrorBoundary>
      </div>,
    );

    expect(screen.getByText('主界面存活标记')).toBeInTheDocument();
    expect(screen.getByText('dialog exploded')).toBeInTheDocument();
    // 不能退化成全屏降级页（那是根边界的行为）
    expect(screen.queryByRole('button', { name: '刷新页面' })).not.toBeInTheDocument();
  });

  it('点击关闭回调宿主的 onClose，让父层卸载对话框', () => {
    const onClose = vi.fn();
    render(
      <DialogErrorBoundary onClose={onClose}>
        <Boom />
      </DialogErrorBoundary>,
    );

    fireEvent.click(screen.getByRole('button', { name: '关闭' }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('未抛错时透传子组件，不额外包裹', () => {
    render(
      <DialogErrorBoundary onClose={vi.fn()}>
        <div>正常内容</div>
      </DialogErrorBoundary>,
    );
    expect(screen.getByText('正常内容')).toBeInTheDocument();
  });
});

describe('ErrorBoundary（根边界）', () => {
  it('捕获后显示全屏降级页并可重试', () => {
    const { rerender } = render(
      <ErrorBoundary>
        <Boom />
      </ErrorBoundary>,
    );
    expect(screen.getByText('界面渲染出现未捕获错误')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '刷新页面' })).toBeInTheDocument();

    // 重试清空错误态：先换成正常子树（否则重试会立刻再次抛错），再点重试
    rerender(
      <ErrorBoundary>
        <div>恢复后的内容</div>
      </ErrorBoundary>,
    );
    fireEvent.click(screen.getByRole('button', { name: '重试' }));
    expect(screen.getByText('恢复后的内容')).toBeInTheDocument();
  });
});
