// 全局快捷键必须在任何模态弹窗打开时让路。
// 旧实现用 e.target.closest('[role="dialog"]') 判断，但弹窗打开后焦点通常
// 仍在背后的触发按钮或 body 上，事件 target 落在弹窗外，判断因此失效。
import { describe, expect, it, afterEach } from 'vitest';
import { isHotkeySuppressed } from './hotkeys';

afterEach(() => {
  document.body.innerHTML = '';
});

describe('isHotkeySuppressed', () => {
  it('页面无弹窗时不拦截', () => {
    expect(isHotkeySuppressed(document.body)).toBe(false);
  });

  it('存在 role=dialog 时拦截，且与事件 target 位置无关', () => {
    const dialog = document.createElement('div');
    dialog.setAttribute('role', 'dialog');
    document.body.append(dialog);

    // target 落在弹窗外（真实场景：焦点留在触发按钮或 body 上）
    expect(isHotkeySuppressed(document.body)).toBe(true);
    // target 落在弹窗内同样拦截
    expect(isHotkeySuppressed(dialog)).toBe(true);
  });

  it('存在 role=alertdialog 时拦截', () => {
    const alert = document.createElement('div');
    alert.setAttribute('role', 'alertdialog');
    document.body.append(alert);

    expect(isHotkeySuppressed(document.body)).toBe(true);
  });

  it('弹窗移除后恢复响应', () => {
    const dialog = document.createElement('div');
    dialog.setAttribute('role', 'dialog');
    document.body.append(dialog);
    expect(isHotkeySuppressed(document.body)).toBe(true);

    dialog.remove();
    expect(isHotkeySuppressed(document.body)).toBe(false);
  });

  it('aria-hidden 的弹窗不算活动弹窗', () => {
    const dialog = document.createElement('div');
    dialog.setAttribute('role', 'dialog');
    dialog.setAttribute('aria-hidden', 'true');
    document.body.append(dialog);

    expect(isHotkeySuppressed(document.body)).toBe(false);
  });

  it('target 为 null 时按有无弹窗判断，不抛错', () => {
    expect(isHotkeySuppressed(null)).toBe(false);

    const dialog = document.createElement('div');
    dialog.setAttribute('role', 'dialog');
    document.body.append(dialog);
    expect(isHotkeySuppressed(null)).toBe(true);
  });
});
