import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import test from 'node:test';
import { fileURLToPath } from 'node:url';

const scriptPath = fileURLToPath(new URL('./set-build-version.mjs', import.meta.url));

function runVersionScript(version, options = {}) {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'apirequest-version-'));
  const configPath = path.join(directory, 'wails.json');
  const githubEnvPath = path.join(directory, 'github-env');
  fs.writeFileSync(configPath, JSON.stringify({ info: { preserved: true } }), 'utf8');

  try {
    const env = { ...process.env, GITHUB_ENV: options.githubEnv ? githubEnvPath : '' };
    const result = spawnSync(
      process.execPath,
      [scriptPath, version, '--config', configPath],
      { encoding: 'utf8', env },
    );
    const config = JSON.parse(fs.readFileSync(configPath, 'utf8'));
    const githubEnv = fs.existsSync(githubEnvPath)
      ? fs.readFileSync(githubEnvPath, 'utf8')
      : '';
    return { ...result, config, githubEnv };
  } finally {
    fs.rmSync(directory, { recursive: true, force: true });
  }
}

test('normalizes stable and fully qualified SemVer versions', () => {
  for (const [input, expected] of [
    ['v1.2.3', '1.2.3'],
    ['2.4.6-beta.1+build.9', '2.4.6'],
    ['255.255.65535', '255.255.65535'],
  ]) {
    const result = runVersionScript(input);
    assert.equal(result.status, 0, result.stderr);
    assert.equal(result.stdout, `${expected}\n`);
    assert.equal(result.config.info.productVersion, expected);
    assert.equal(result.config.info.preserved, true);
  }
});

test('rejects malformed and out-of-range Windows Installer versions', () => {
  for (const input of ['1.2', '1.2.3-', '256.0.0', '1.256.0', '1.0.65536']) {
    const result = runVersionScript(input);
    assert.notEqual(result.status, 0, `${input} should fail`);
  }
});

test('exports the normalized version to GITHUB_ENV', () => {
  const result = runVersionScript('v3.2.1-rc.1+build.7', { githubEnv: true });
  assert.equal(result.status, 0, result.stderr);
  assert.equal(result.githubEnv, 'APP_VERSION=3.2.1\n');
});
