// 包体预算检查：dist/ 下所有发布包（EXE/ZIP/MSI/DMG/Linux 可执行）单平台不得超 30MB
//（docs/ops.md §5 性能预算）。CI 在上传/发布前调用，超限即失败。
// 非包文件（SHA256SUMS、SIGNING_STATUS-*.txt 等）不参与预算。
import fs from 'node:fs';
import path from 'node:path';

export const budgetLimitBytes = 30 * 1024 * 1024;

// 预算只约束面向用户的发布包；STATUS/SUMS 是 CI 元数据
function isPackage(name) {
  return /^ApiRequest-.*(\.exe|\.zip|\.msi|\.dmg)$/.test(name) || /^ApiRequest-[^.]*$/.test(name);
}

export function listOversized(files) {
  return files
    .filter((f) => isPackage(f.name) && f.size > budgetLimitBytes)
    .map((f) => ({ ...f }));
}

export function formatSizeLine(f) {
  return `${f.name}: ${(f.size / (1024 * 1024)).toFixed(1)} MiB`;
}

function main(dir) {
  if (!fs.existsSync(dir)) {
    console.error(`dist budget: directory not found: ${dir}`);
    process.exit(2);
  }
  const files = fs
    .readdirSync(dir)
    .filter((name) => fs.statSync(path.join(dir, name)).isFile())
    .map((name) => ({ name, size: fs.statSync(path.join(dir, name)).size }));
  const packages = files.filter((f) => isPackage(f.name));
  if (packages.length === 0) {
    console.error('dist budget: no ApiRequest-* packages found (nothing to check)');
    process.exit(2);
  }
  const oversized = listOversized(files);
  for (const f of packages) {
    const flag = f.size > budgetLimitBytes ? ' OVER BUDGET' : '';
    console.log(`  ${formatSizeLine(f)} / ${(budgetLimitBytes / (1024 * 1024)).toFixed(0)} MiB${flag}`);
  }
  if (oversized.length > 0) {
    console.error(`\ndist budget: ${oversized.length} package(s) exceed 30 MiB (docs/ops.md §5):`);
    for (const f of oversized) console.error(`  ${formatSizeLine(f)}`);
    process.exit(1);
  }
  console.log(`dist budget: all ${packages.length} package(s) within 30 MiB`);
}

if (process.argv[1] && import.meta.url === new URL(`file://${path.resolve(process.argv[1])}`).href) {
  const dir = process.argv[2] || 'dist';
  main(dir);
}
