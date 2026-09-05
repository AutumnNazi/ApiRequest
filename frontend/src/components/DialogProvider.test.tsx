import { StrictMode, useState } from 'react';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
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

describe('DialogProvider queueing', () => {
  function QueueHarness() {
    const dialog = useDialog();
    const [resolved, setResolved] = useState<string[]>([]);
    return (
      <>
        <button
          onClick={() => {
            void dialog.confirm('A?').then((ok) => setResolved((prev) => [...prev, `A:${ok}`]));
            void dialog.confirm('B?').then((ok) => setResolved((prev) => [...prev, `B:${ok}`]));
          }}
        >
          open twice
        </button>
        <output>{resolved.join(',')}</output>
      </>
    );
  }

  // 必须在 StrictMode 下跑：入队逻辑若留在 setState updater 里，StrictMode 的
  // 双调用会让同一弹窗重复入队，这是本用例要守住的回归。
  it('queues concurrent dialogs exactly once each under StrictMode', async () => {
    render(
      <StrictMode>
        <DialogProvider>
          <QueueHarness />
        </DialogProvider>
      </StrictMode>,
    );
    fireEvent.click(screen.getByRole('button', { name: 'open twice' }));

    // 第一个弹窗（A）激活，B 在队列中等待
    expect(await screen.findByText('A?')).toBeInTheDocument();
    expect(screen.queryByText('B?')).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: '确定' }));

    // A 出队后 B 恰好激活一次
    expect(await screen.findByText('B?')).toBeInTheDocument();
    expect(screen.queryByText('A?')).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: '确定' }));

    // 两个 promise 各 resolve 一次，且队列排空（重复入队会在此留下第三个弹窗）
    await waitFor(() => expect(screen.getByText('A:true,B:true')).toBeInTheDocument());
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });
});

describe('DialogProvider finish idempotency', () => {
  function TripleHarness() {
    const dialog = useDialog();
    const [resolved, setResolved] = useState<string[]>([]);
    return (
      <>
        <button
          onClick={() => {
            void dialog.confirm('first?').then((ok) => setResolved((p) => [...p, `1:${ok}`]));
            void dialog.confirm('second?').then((ok) => setResolved((p) => [...p, `2:${ok}`]));
            void dialog.confirm('third?').then((ok) => setResolved((p) => [...p, `3:${ok}`]));
          }}
        >
          open three
        </button>
        <output>{resolved.join(',')}</output>
      </>
    );
  }

  // finish 必须幂等：同一弹窗被同步关闭两次（Enter 触发 submit 又冒泡到按钮 click）
  // 若各自 shift 一次队列，排队中的下一个弹窗会被静默吞掉、其 promise 永不 resolve。
  it('does not drop a queued dialog when finish fires twice synchronously', async () => {
    render(
      <DialogProvider>
        <TripleHarness />
      </DialogProvider>,
    );
    fireEvent.click(screen.getByRole('button', { name: 'open three' }));
    expect(await screen.findByText('first?')).toBeInTheDocument();

    // 同步连发两次 submit，模拟 Enter + click 双路径
    // 两次派发必须在同一个 act 内：fireEvent 各自会刷新状态，那样第二次打到的是
    // 重渲染后的表单（闭包已换成下一个弹窗），属于合法的独立确认，测不到本缺陷。
    const form = screen.getByRole('dialog').querySelector('form') as HTMLFormElement;
    const submitOnce = () =>
      form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    act(() => {
      submitOnce();
      submitOnce();
    });

    // 第二个弹窗必须还在（被吞掉时这里会直接跳到 third?）
    expect(await screen.findByText('second?')).toBeInTheDocument();
    fireEvent.submit(screen.getByRole('dialog').querySelector('form') as HTMLFormElement);

    expect(await screen.findByText('third?')).toBeInTheDocument();
    fireEvent.submit(screen.getByRole('dialog').querySelector('form') as HTMLFormElement);

    // 三个 promise 全部 resolve，无一丢失
    await waitFor(() => expect(screen.getByText('1:true,2:true,3:true')).toBeInTheDocument());
  });
});
