import test from 'node:test';
import assert from 'node:assert/strict';
import { budgetLimitBytes, listOversized, formatSizeLine } from './check-dist-budget.mjs';

test('budget limit is 35 MiB', () => {
  assert.equal(budgetLimitBytes, 35 * 1024 * 1024);
});

test('oversized packages are flagged with sizes', () => {
  const files = [
    { name: 'ApiRequest-dev-1-Windows-Amd64-Portable.exe', size: 34 * 1024 * 1024 },
    { name: 'ApiRequest-dev-1-MacOS-Arm64.dmg', size: 36 * 1024 * 1024 },
  ];
  const flagged = listOversized(files);
  assert.equal(flagged.length, 1);
  assert.equal(flagged[0].name, 'ApiRequest-dev-1-MacOS-Arm64.dmg');
});

test('non-package files are ignored', () => {
  const files = [
    { name: 'SHA256SUMS', size: 1024 },
    { name: 'SIGNING_STATUS-windows-amd64.txt', size: 512 },
    { name: 'ApiRequest-dev-1-Linux-Amd64', size: 60 * 1024 * 1024 },
  ];
  const flagged = listOversagedSafe(files);
  assert.equal(flagged.length, 1);
  assert.equal(flagged[0].name, 'ApiRequest-dev-1-Linux-Amd64');
});

function listOversagedSafe(files) {
  // sanity: same behavior as listOversized but keeps the test readable
  return listOversized(files);
}

test('formats size report line', () => {
  const line = formatSizeLine({ name: 'a.zip', size: 31 * 1024 * 1024 });
  assert.match(line, /a\.zip/);
  assert.match(line, /31\.0 MiB/);
});
