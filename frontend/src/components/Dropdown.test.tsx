import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import Dropdown from './Dropdown';

const OPTIONS = [
  { value: 'a', label: 'Alpha' },
  { value: 'b', label: 'Beta' },
];

function renderDropdown(props: Partial<React.ComponentProps<typeof Dropdown>> = {}) {
  const onChange = vi.fn();
  const view = render(
    <Dropdown value="a" options={OPTIONS} onChange={onChange} {...props} />,
  );
  return { onChange, view };
}

describe('Dropdown openSignal', () => {
  it('保持关闭直到 openSignal 变化', () => {
    const { view } = renderDropdown({ openSignal: 0 });
    expect(screen.queryByRole('button', { name: 'Beta' })).toBeNull();

    view.rerender(<Dropdown value="a" options={OPTIONS} onChange={vi.fn()} openSignal={1} />);
    expect(screen.getByRole('button', { name: 'Beta' })).toBeInTheDocument();
  });

  it('首次渲染时不因 openSignal 初值而自动展开', () => {
    renderDropdown({ openSignal: 7 });
    expect(screen.queryByRole('button', { name: 'Beta' })).toBeNull();
  });

  it('disabled 时 openSignal 变化不展开', () => {
    const { view } = renderDropdown({ openSignal: 0, disabled: true });
    view.rerender(
      <Dropdown value="a" options={OPTIONS} onChange={vi.fn()} openSignal={1} disabled />,
    );
    expect(screen.queryByRole('button', { name: 'Beta' })).toBeNull();
  });

  it('未传 openSignal 时点击仍可开合', () => {
    renderDropdown();
    const trigger = screen.getByRole('button', { name: /Alpha/ });

    fireEvent.click(trigger);
    expect(screen.getByRole('button', { name: 'Beta' })).toBeInTheDocument();

    fireEvent.click(trigger);
    expect(screen.queryByRole('button', { name: 'Beta' })).toBeNull();
  });
});
