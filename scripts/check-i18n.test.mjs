import assert from 'node:assert/strict';
import test from 'node:test';

const { collectUsedKeys, collectDefinedKeys, diffKeys } = await import('./check-i18n.mjs');

test('collectUsedKeys extracts literal keys and unescapes quotes', () => {
  const source = [
    "const a = formatMessage('保存');",
    "const b = formatMessage('删除 {count}', { count: 1 });",
    "const c = formatMessage(\n  '跨行键',\n  {},\n);",
    "const d = formatMessage(`dynamic ${x}`);", // 模板串：忽略
    "const e = formatMessage(variable);", // 动态：忽略
    "const f = formatMessage('带\\'引号');",
  ].join('\n');
  assert.deepEqual(collectUsedKeys(source), ['保存', '删除 {count}', '跨行键', "带'引号"]);
});

test('collectDefinedKeys reads top-level entries only', () => {
  const source = [
    "export const messages = {",
    "  '已保存': 'Saved',",
    "    '深缩进不是顶层键': 'x',",
    "};",
    "// '注释里的键': 'noisy',",
  ].join('\n');
  assert.deepEqual(collectDefinedKeys(source), ['已保存']);
});

test('diffKeys reports missing and duplicates', () => {
  const { missing, duplicates } = diffKeys(
    ['a', 'b', 'c', 'c'],
    ['a', 'b', 'b'],
  );
  assert.deepEqual(missing, ['c']);
  assert.deepEqual(duplicates, ['b']);
});
