import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import QueryErrorState from './QueryErrorState';

describe('QueryErrorState', () => {
  it('shows verbatim error detail and retries on demand', () => {
    const onRetry = vi.fn();
    render(
      <QueryErrorState
        message="工作区加载失败"
        detail="database 保存 failed"
        onRetry={onRetry}
      />,
    );

    expect(screen.getByRole('alert')).toHaveTextContent('工作区加载失败');
    expect(screen.getByRole('alert')).toHaveTextContent('database 保存 failed');
    fireEvent.click(screen.getByRole('button', { name: '重试' }));
    expect(onRetry).toHaveBeenCalledOnce();
  });
});
