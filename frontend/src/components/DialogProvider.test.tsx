import { useState } from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { DialogProvider, useDialog } from './DialogProvider';

function Harness() {
  const dialog = useDialog();
  const [result, setResult] = useState('');
  return (
    <>
      <button onClick={() => void dialog.confirm('确认测试？').then((ok) => setResult(String(ok)))}>
        open confirm
      </button>
      <button onClick={() => void dialog.prompt('输入测试', { defaultValue: '初始值' }).then((value) => setResult(value ?? 'cancelled'))}>
        open prompt
      </button>
      <output>{result}</output>
    </>
  );
}

describe('DialogProvider', () => {
  it('resolves confirm and restores focus', async () => {
    render(
      <DialogProvider>
        <Harness />
      </DialogProvider>,
    );
    const trigger = screen.getByRole('button', { name: 'open confirm' });
    trigger.focus();
    fireEvent.click(trigger);
    expect(screen.getByRole('dialog')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: '确定' }));
    await waitFor(() => expect(screen.getByText('true')).toBeInTheDocument());
    expect(document.activeElement).toBe(trigger);
  });

  it('returns prompt text and supports Escape cancellation', async () => {
    render(
      <DialogProvider>
        <Harness />
      </DialogProvider>,
    );
    fireEvent.click(screen.getByRole('button', { name: 'open prompt' }));
    const input = screen.getByRole('textbox');
    expect(input).toHaveValue('初始值');
    fireEvent.change(input, { target: { value: '新值' } });
    fireEvent.click(screen.getByRole('button', { name: '确定' }));
    await waitFor(() => expect(screen.getByText('新值')).toBeInTheDocument());

    fireEvent.click(screen.getByRole('button', { name: 'open prompt' }));
    fireEvent.keyDown(document, { key: 'Escape' });
    await waitFor(() => expect(screen.getByText('cancelled')).toBeInTheDocument());
  });
});
