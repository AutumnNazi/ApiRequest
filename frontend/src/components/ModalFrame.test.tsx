import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import ModalFrame from './ModalFrame';

describe('ModalFrame', () => {
  it('closes from Escape and the backdrop but not from content clicks', () => {
    const onClose = vi.fn();
    render(
      <ModalFrame className="modal" onClose={onClose} titleId="modal-title">
        <h2 id="modal-title">Dialog</h2>
        <button>Inside</button>
      </ModalFrame>,
    );

    fireEvent.click(screen.getByRole('button', { name: 'Inside' }));
    expect(onClose).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('dialog').parentElement!);
    expect(onClose).toHaveBeenCalledTimes(1);
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(onClose).toHaveBeenCalledTimes(2);
  });

  it('traps Tab focus and restores the previous focus on unmount', async () => {
    const previous = document.createElement('button');
    document.body.appendChild(previous);
    previous.focus();
    const { unmount } = render(
      <ModalFrame className="modal" onClose={() => undefined} ariaLabel="Dialog">
        <button>First</button>
        <button>Last</button>
      </ModalFrame>,
    );

    const first = await screen.findByRole('button', { name: 'First' });
    const last = screen.getByRole('button', { name: 'Last' });
    await vi.waitFor(() => expect(first).toHaveFocus());
    last.focus();
    fireEvent.keyDown(document, { key: 'Tab' });
    expect(first).toHaveFocus();
    fireEvent.keyDown(document, { key: 'Tab', shiftKey: true });
    expect(last).toHaveFocus();

    unmount();
    expect(previous).toHaveFocus();
    previous.remove();
  });

  it('leaves Escape to a dialog rendered above it', () => {
    const onClose = vi.fn();
    render(
      <>
        <ModalFrame className="modal" onClose={onClose} ariaLabel="Underlying dialog">
          <button>Underlying action</button>
        </ModalFrame>
        <div role="alertdialog" aria-modal="true">
          Confirm
        </div>
      </>,
    );

    fireEvent.keyDown(document, { key: 'Escape' });
    expect(onClose).not.toHaveBeenCalled();
  });
});
