// i18n 键覆盖检查（CI 门禁）：扫描组件里 formatMessage('...') 的键，
// 对比 frontend/src/i18n/messages.en.ts 的定义——漏定义/重复定义即失败。
// 用法：node scripts/check-i18n.mjs
import fs from 'node:fs';
import path from 'node:path';
import process from 'node:process';
import { fileURLToPath } from 'node:url';

const repoRoot = path.resolve(fileURLToPath(new URL('..', import.meta.url)));
const frontendSrc = path.join(repoRoot, 'frontend', 'src');
const messagesPath = path.join(frontendSrc, 'i18n', 'messages.en.ts');

// formatMessage('...')：单引号字面量键（模板串/动态键无法静态验证，跳过）
const keyPattern = /formatMessage\(\s*'((?:[^'\\]|\\.)*)'/g;
// messages.en.ts 顶层键：形如 ^  '键': '译文',
const definedPattern = /^  '((?:[^'\\]|\\.)*)':/gm;

export function collectUsedKeys(source) {
  const keys = [];
  for (const match of source.matchAll(keyPattern)) {
    keys.push(match[1].replace(/\\'/g, "'"));
  }
  return keys;
}

export function collectDefinedKeys(source) {
  const keys = [];
  for (const match of source.matchAll(definedPattern)) {
    keys.push(match[1].replace(/\\'/g, "'"));
  }
  return keys;
}

export function diffKeys(used, defined) {
  const definedSet = new Set(defined);
  const missing = [...new Set(used)].filter((key) => !definedSet.has(key)).sort();
  const seen = new Set();
  const duplicates = [];
  for (const key of defined) {
    if (seen.has(key) && !duplicates.includes(key)) duplicates.push(key);
    seen.add(key);
  }
  return { missing, duplicates };
}

export function runCheck({ srcDir = frontendSrc, messagesFile = messagesPath } = {}) {
  const definedSource = fs.readFileSync(messagesFile, 'utf8');
  const defined = collectDefinedKeys(definedSource);

  const used = [];
  const walk = (dir) => {
    for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
      const full = path.join(dir, entry.name);
      if (entry.isDirectory()) {
        walk(full);
        continue;
      }
      if (!entry.name.endsWith('.ts') && !entry.name.endsWith('.tsx')) continue;
      if (entry.name.endsWith('.test.ts') || entry.name.endsWith('.test.tsx')) continue;
      if (full === messagesFile) continue;
      used.push(...collectUsedKeys(fs.readFileSync(full, 'utf8')));
    }
  };
  walk(srcDir);

  return diffKeys(used, defined);
}

function main() {
  const { missing, duplicates } = runCheck();
  let failed = false;
  if (missing.length > 0) {
    failed = true;
    console.error(`[check-i18n] ${missing.length} 个 formatMessage 键未在 messages.en.ts 定义：`);
    for (const key of missing) console.error(`  - ${key}`);
  }
  if (duplicates.length > 0) {
    failed = true;
    console.error(`[check-i18n] messages.en.ts 存在 ${duplicates.length} 个重复键：`);
    for (const key of duplicates) console.error(`  - ${key}`);
  }
  if (!failed) {
    console.log('[check-i18n] i18n key coverage OK');
    return;
  }
  process.exit(1);
}

if (process.argv[1] && process.argv[1].endsWith('check-i18n.mjs')) {
  main();
}
