import { render, fireEvent } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import Splitter from './Splitter';

// 拖拽依赖 window 尺寸换算比例，jsdom 默认 1024x768。
function splitterOf(container: HTMLElement) {
  const node = container.firstElementChild;
  if (!(node instanceof HTMLElement)) throw new Error('splitter 未渲染');
  return node;
}

describe('Splitter 拖拽', () => {
  it('vertical：mousedown 后移动鼠标按 clientX 增量回调新比例', () => {
    const onRatio = vi.fn();
    const { container } = render(<Splitter orientation="vertical" ratio={0.25} onRatio={onRatio} />);

    fireEvent.mouseDown(splitterOf(container), { clientX: 256 });
    fireEvent.mouseMove(window, { clientX: 256 + window.innerWidth * 0.1 });

    expect(onRatio).toHaveBeenCalledWith(expect.closeTo(0.35, 5));
  });

  it('horizontal：按 clientY 增量回调，忽略 clientX', () => {
    const onRatio = vi.fn();
    const { container } = render(<Splitter orientation="horizontal" ratio={0.5} onRatio={onRatio} />);

    fireEvent.mouseDown(splitterOf(container), { clientY: 384 });
    fireEvent.mouseMove(window, { clientY: 384 - window.innerHeight * 0.2, clientX: 999 });

    expect(onRatio).toHaveBeenCalledWith(expect.closeTo(0.3, 5));
  });

  it('比例夹在 0.15~0.85 之间', () => {
    const onRatio = vi.fn();
    const { container } = render(<Splitter orientation="vertical" ratio={0.5} onRatio={onRatio} />);

    fireEvent.mouseDown(splitterOf(container), { clientX: 500 });
    fireEvent.mouseMove(window, { clientX: 500 + window.innerWidth });
    expect(onRatio).toHaveBeenLastCalledWith(0.85);

    fireEvent.mouseMove(window, { clientX: 500 - window.innerWidth });
    expect(onRatio).toHaveBeenLastCalledWith(0.15);
  });

  it('mouseup 后停止响应移动', () => {
    const onRatio = vi.fn();
    const { container } = render(<Splitter orientation="vertical" ratio={0.25} onRatio={onRatio} />);

    fireEvent.mouseDown(splitterOf(container), { clientX: 256 });
    fireEvent.mouseMove(window, { clientX: 300 });
    const callsBeforeRelease = onRatio.mock.calls.length;
    expect(callsBeforeRelease).toBeGreaterThan(0);

    fireEvent.mouseUp(window);
    fireEvent.mouseMove(window, { clientX: 400 });

    expect(onRatio).toHaveBeenCalledTimes(callsBeforeRelease);
  });

  it('mouseup 后恢复 body 的 cursor 与 userSelect', () => {
    const { container } = render(<Splitter orientation="vertical" ratio={0.25} onRatio={vi.fn()} />);

    fireEvent.mouseDown(splitterOf(container), { clientX: 256 });
    expect(document.body.style.cursor).toBe('col-resize');
    expect(document.body.style.userSelect).toBe('none');

    fireEvent.mouseUp(window);
    expect(document.body.style.cursor).toBe('');
    expect(document.body.style.userSelect).toBe('');
  });

  it('拖拽中卸载组件不残留监听器与 body 样式', () => {
    const onRatio = vi.fn();
    const { container, unmount } = render(
      <Splitter orientation="vertical" ratio={0.25} onRatio={onRatio} />,
    );

    fireEvent.mouseDown(splitterOf(container), { clientX: 256 });
    unmount();
    onRatio.mockClear();

    fireEvent.mouseMove(window, { clientX: 400 });

    expect(onRatio).not.toHaveBeenCalled();
    expect(document.body.style.cursor).toBe('');
    expect(document.body.style.userSelect).toBe('');
  });
});

// 松手结算：onDragEnd 以最终比例调用一次；拖拽期间不重复调用
describe('Splitter 拖拽结束回调', () => {
  it('mouseup 后以最终 ratio 调用 onDragEnd 恰一次', () => {
    const onRatio = vi.fn();
    const onDragEnd = vi.fn();
    const { container } = render(
      <Splitter orientation="vertical" ratio={0.25} onRatio={onRatio} onDragEnd={onDragEnd} />,
    );
    fireEvent.mouseDown(splitterOf(container), { clientX: 300 });
    fireEvent.mouseMove(window, { clientX: 300 + window.innerWidth * 0.1 });
    fireEvent.mouseMove(window, { clientX: 300 + window.innerWidth * 0.2 });
    expect(onDragEnd).not.toHaveBeenCalled();
    fireEvent.mouseUp(window);
    expect(onDragEnd).toHaveBeenCalledTimes(1);
    expect(onDragEnd).toHaveBeenCalledWith(0.45);
  });

  it('未拖动直接松手不调用 onDragEnd', () => {
    const onDragEnd = vi.fn();
    const { container } = render(
      <Splitter orientation="vertical" ratio={0.25} onRatio={vi.fn()} onDragEnd={onDragEnd} />,
    );
    fireEvent.mouseDown(splitterOf(container), { clientX: 300 });
    fireEvent.mouseUp(window);
    expect(onDragEnd).not.toHaveBeenCalled();
  });

  it('clamp 后的 ratio 才是结算值（拖出边界）', () => {
    const onDragEnd = vi.fn();
    const { container } = render(
      <Splitter orientation="vertical" ratio={0.5} onRatio={vi.fn()} onDragEnd={onDragEnd} />,
    );
    fireEvent.mouseDown(splitterOf(container), { clientX: 500 });
    fireEvent.mouseMove(window, { clientX: 500 + window.innerWidth }); // 远超 0.85 上限
    fireEvent.mouseUp(window);
    expect(onDragEnd).toHaveBeenCalledWith(0.85);
  });
});
