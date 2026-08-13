import { fireEvent, render } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import type { KV } from '../ipc';
import KVTable from './KVTable';

describe('KVTable row identity', () => {
  it('preserves the following input DOM node, focus, and cursor after deleting an earlier row', () => {
    let items: KV[] = [
      { key: 'a', value: 'alpha', enabled: true, description: '' },
      { key: 'b', value: 'bravo', enabled: true, description: '' },
      { key: 'c', value: 'charlie', enabled: true, description: '' },
    ];
    const view = render(
      <KVTable
        items={items}
        onChange={(next) => {
          items = next;
          view.rerender(<KVTable items={items} onChange={() => undefined} />);
        }}
      />,
    );
    const valueInputs = view.getAllByPlaceholderText('Value') as HTMLInputElement[];
    const focused = valueInputs[1];
    focused.focus();
    focused.setSelectionRange(2, 2);

    fireEvent.click(view.getAllByTitle('删除')[0]);

    const nextValueInputs = view.getAllByPlaceholderText('Value') as HTMLInputElement[];
    expect(nextValueInputs[0]).toBe(focused);
    expect(document.activeElement).toBe(focused);
    expect(focused.value).toBe('bravo');
    expect(focused.selectionStart).toBe(2);
  });

  it('highlights duplicate keys with a red warning class', () => {
    let items: KV[] = [
      { key: 'X-Token', value: 'first', enabled: true, description: '' },
      { key: 'x-token', value: 'second', enabled: true, description: '' },
      { key: 'Unique', value: 'third', enabled: true, description: '' },
    ];
    let onChangeCalls = 0;
    const view = render(
      <KVTable
        items={items}
        onChange={(next) => {
          items = next;
          onChangeCalls++;
          if (onChangeCalls > 0) {
            view.rerender(<KVTable items={items} onChange={() => undefined} />);
          }
        }}
      />,
    );
    const keyInputs = view.getAllByPlaceholderText('Key') as HTMLInputElement[];
    // 前两行重复（大小写不敏感），第三行不重复（注意末行是 ghost 行）
    expect(keyInputs[0].className).toContain('bg-red-50');
    expect(keyInputs[1].className).toContain('bg-red-50');
    expect(keyInputs[2].className).not.toContain('bg-red-50');
    expect(keyInputs[0].title).toContain('重复');
  });
});
