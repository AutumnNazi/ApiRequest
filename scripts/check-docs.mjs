import fs from 'node:fs';
import path from 'node:path';
import process from 'node:process';
import { fileURLToPath } from 'node:url';

const repoRoot = path.resolve(fileURLToPath(new URL('..', import.meta.url)));
const docsDirectory = path.join(repoRoot, 'docs');
const englishDocsDirectory = path.join(docsDirectory, 'en');
const markdownFiles = [];

function collectMarkdown(directory) {
  for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
    if (entry.name === '.git' || entry.name === 'node_modules') continue;
    const fullPath = path.join(directory, entry.name);
    if (entry.isDirectory()) {
      collectMarkdown(fullPath);
    } else if (entry.isFile() && entry.name.endsWith('.md')) {
      markdownFiles.push(fullPath);
    }
  }
}

function isWithin(child, parent) {
  const relative = path.relative(parent, child);
  return relative === '' || (!relative.startsWith('..') && !path.isAbsolute(relative));
}

function extractTargets(markdown) {
  const targets = [];
  const patterns = [
    /!?\[[^\]]*\]\((<[^>]+>|[^\s)]+)(?:\s+["'][^"']*["'])?\)/g,
    /^\s*\[[^\]]+\]:\s*(<[^>]+>|[^\s]+)\s*/gm,
    /(?:href|src)=["']([^"']+)["']/gi,
  ];
  for (const pattern of patterns) {
    for (const match of markdown.matchAll(pattern)) {
      targets.push(match[1].replace(/^<|>$/g, ''));
    }
  }
  return targets;
}

function resolveLocalTarget(sourceFile, target) {
  if (!target || target.startsWith('#') || target.startsWith('//')) return null;
  if (/^[A-Za-z][A-Za-z0-9+.-]*:/.test(target)) return null;
  const pathname = target.split(/[?#]/, 1)[0];
  if (!pathname) return null;
  const decoded = decodeURIComponent(pathname);
  return path.resolve(target.startsWith('/') ? repoRoot : path.dirname(sourceFile), decoded.replace(/^[/\\]+/, ''));
}

function expectedLanguagePeer(file) {
  if (file === path.join(repoRoot, 'README.md')) return path.join(repoRoot, 'README.zh-CN.md');
  if (file === path.join(repoRoot, 'README.zh-CN.md')) return path.join(repoRoot, 'README.md');
  if (path.dirname(file) === englishDocsDirectory) return path.join(docsDirectory, path.basename(file));
  if (path.dirname(file) === docsDirectory) return path.join(englishDocsDirectory, path.basename(file));
  return null;
}

collectMarkdown(repoRoot);
markdownFiles.sort();

const failures = [];
const resolvedTargetsByFile = new Map();
for (const file of markdownFiles) {
  const targets = extractTargets(fs.readFileSync(file, 'utf8'));
  const resolvedTargets = [];
  for (const target of targets) {
    let resolved;
    try {
      resolved = resolveLocalTarget(file, target);
    } catch {
      failures.push(`${path.relative(repoRoot, file)}: invalid link encoding: ${target}`);
      continue;
    }
    if (!resolved) continue;
    resolvedTargets.push(resolved);
    if (!isWithin(resolved, repoRoot)) {
      failures.push(`${path.relative(repoRoot, file)}: link escapes the repository: ${target}`);
    } else if (!fs.existsSync(resolved)) {
      failures.push(`${path.relative(repoRoot, file)}: missing local target: ${target}`);
    }
  }
  resolvedTargetsByFile.set(file, resolvedTargets);
}

for (const file of markdownFiles) {
  const peer = expectedLanguagePeer(file);
  if (peer && !fs.existsSync(peer)) {
    failures.push(`${path.relative(repoRoot, file)}: missing language peer ${path.relative(repoRoot, peer)}`);
  } else if (peer && !resolvedTargetsByFile.get(file).includes(peer)) {
    failures.push(`${path.relative(repoRoot, file)}: missing language switch to ${path.relative(repoRoot, peer)}`);
  }
}

const englishReadme = path.join(repoRoot, 'README.md');
for (const [source, targets] of resolvedTargetsByFile) {
  const isEnglishSource = source === englishReadme || path.dirname(source) === englishDocsDirectory;
  if (!isEnglishSource) continue;
  const peer = expectedLanguagePeer(source);
  for (const target of targets) {
    const pointsToChineseDoc = path.dirname(target) === docsDirectory && target.endsWith('.md');
    const pointsToChineseReadme = target === path.join(repoRoot, 'README.zh-CN.md');
    if ((pointsToChineseDoc || pointsToChineseReadme) && target !== peer) {
      failures.push(`${path.relative(repoRoot, source)}: English content links to Chinese content: ${path.relative(repoRoot, target)}`);
    }
  }
}

if (failures.length > 0) {
  for (const failure of failures) console.error(failure);
  process.exit(1);
}

process.stdout.write(`Checked ${markdownFiles.length} Markdown files and bilingual routes.\n`);
