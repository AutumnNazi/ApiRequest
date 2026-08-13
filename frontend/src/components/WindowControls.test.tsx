import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import WindowControls from './WindowControls';

const runtime = vi.hoisted(() => ({
  WindowMinimise: vi.fn(),
  WindowToggleMaximise: vi.fn(),
  WindowIsMaximised: vi.fn<() => Promise<boolean>>(),
  Quit: vi.fn(),
}));

vi.mock('../../wailsjs/runtime/runtime', () => runtime);

describe('WindowControls', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    runtime.WindowIsMaximised.mockResolvedValue(false);
  });

  it('uses consistent control dimensions and accessible names', async () => {
    render(<WindowControls onClose={() => {}} />);

    const controls = screen.getByRole('group', { name: '窗口控制' });
    const buttons = screen.getAllByRole('button');
    expect(controls).toContainElement(buttons[0]);
    expect(buttons).toHaveLength(3);
    for (const button of buttons) {
      expect(button).toHaveClass(
        'h-10',
        'w-11',
        'p-0',
        'text-gray-500',
        'focus-visible:ring-gray-400',
      );
      expect(button).not.toHaveClass('focus-visible:ring-blue-500');
    }
    expect(screen.getByRole('button', { name: '最小化' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '最大化' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '关闭' })).toBeInTheDocument();
    await waitFor(() => expect(runtime.WindowIsMaximised).toHaveBeenCalledOnce());
  });

  it('calls the window actions and delegates close through the application guard', async () => {
    const onClose = vi.fn();
    runtime.WindowIsMaximised
      .mockResolvedValueOnce(false)
      .mockResolvedValueOnce(true);
    render(<WindowControls onClose={onClose} />);
    await waitFor(() => expect(runtime.WindowIsMaximised).toHaveBeenCalledOnce());

    fireEvent.click(screen.getByRole('button', { name: '最小化' }));
    expect(runtime.WindowMinimise).toHaveBeenCalledOnce();

    fireEvent.click(screen.getByRole('button', { name: '最大化' }));
    expect(runtime.WindowToggleMaximise).toHaveBeenCalledOnce();
    await waitFor(() => {
      expect(screen.getByRole('button', { name: '还原' })).toBeInTheDocument();
    });

    fireEvent(window, new Event('resize'));
    await waitFor(() => expect(runtime.WindowIsMaximised).toHaveBeenCalledTimes(2));

    const restoreButton = screen.getByRole('button', { name: '还原' });
    const restoreLayers = restoreButton.querySelectorAll('[aria-hidden="true"] > span');
    expect(restoreLayers).toHaveLength(2);
    expect(restoreLayers[0]).toHaveClass('border-r', 'border-t', 'border-current');
    expect(restoreLayers[0]).not.toHaveClass('border');
    for (const layer of restoreLayers) {
      expect(layer).not.toHaveClass('bg-white', 'group-hover:bg-gray-100');
    }

    fireEvent.click(screen.getByRole('button', { name: '关闭' }));
    expect(onClose).toHaveBeenCalledOnce();
    expect(runtime.Quit).not.toHaveBeenCalled();
  });
});
