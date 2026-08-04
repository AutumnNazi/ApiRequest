import fs from 'node:fs';
import path from 'node:path';
import process from 'node:process';

const args = process.argv.slice(2);
let configPath = 'wails.json';
const configIndex = args.indexOf('--config');
if (configIndex !== -1) {
  if (!args[configIndex + 1]) throw new Error('--config requires a path');
  configPath = args[configIndex + 1];
  args.splice(configIndex, 2);
}
if (args.length !== 1) {
  throw new Error('usage: node scripts/set-build-version.mjs <version> [--config <path>]');
}

const match = /^v?(\d+)\.(\d+)\.(\d+)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$/.exec(args[0]);
if (!match) throw new Error(`invalid build version: ${args[0]}`);

const parts = match.slice(1).map(Number);
if (parts[0] > 255 || parts[1] > 255 || parts[2] > 65535) {
  throw new Error(`version exceeds Windows Installer limits: ${args[0]}`);
}
const version = parts.join('.');
const absoluteConfigPath = path.resolve(configPath);
const config = JSON.parse(fs.readFileSync(absoluteConfigPath, 'utf8'));
config.info = {
  ...config.info,
  companyName: 'chenkaiyuan',
  productName: 'ApiRequest',
  productVersion: version,
  copyright: `Copyright (c) ${new Date().getUTCFullYear()} chenkaiyuan`,
  comments: 'Native API development client built with Wails',
};
fs.writeFileSync(absoluteConfigPath, `${JSON.stringify(config, null, 2)}\n`, 'utf8');

if (process.env.GITHUB_ENV) {
  fs.appendFileSync(process.env.GITHUB_ENV, `APP_VERSION=${version}\n`, 'utf8');
}
process.stdout.write(`${version}\n`);
