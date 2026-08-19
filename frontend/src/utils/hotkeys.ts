// 全局快捷键的抑制判定。
//
// 早期实现用 event.target.closest('[role="dialog"]')，但弹窗打开时焦点通常仍留在
// 背后的触发按钮或 body 上，事件 target 落在弹窗外，判断随即失效、快捷键穿透。
// 因此改为查询文档内是否存在活动弹窗，与事件 target 的位置解耦。
const MODAL_SELECTOR =
  '[role="dialog"]:not([aria-hidden="true"]), [role="alertdialog"]:not([aria-hidden="true"])';

// target 参数保留用于将来按落点细化策略（如放行输入框内的编辑类快捷键）；
// 当前只要页面存在活动弹窗就一律抑制。
export function isHotkeySuppressed(_target: EventTarget | null): boolean {
  return document.querySelector(MODAL_SELECTOR) !== null;
}
