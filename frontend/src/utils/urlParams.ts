// URL 与 Params 表格的双向同步（docs/roadmap.md 请求构造）。
// 序列化必须保持 {{var}} 模板原样：变量解析发生在发送前的原始串上
// （backend/template/resolve.go 找的是字面量 "{{"），百分号编码会破坏解析。
const TEMPLATE_RE = /\{\{[^}]*\}\}/g;
const STRUCTURAL_ESCAPES: Record<string, string> = {
  '&': '%26',
  '#': '%23',
  '+': '%2B',
  '%': '%25',
  ' ': '%20',
};

/** 拆出 base / query / fragment 三段；query 为 '?' 之后到 '#' 之前的原文。 */
export function splitUrlParts(raw: string): { base: string; query: string; fragment: string } {
  const hashIdx = raw.indexOf('#');
  const fragment = hashIdx >= 0 ? raw.slice(hashIdx) : '';
  const withoutFragment = hashIdx >= 0 ? raw.slice(0, hashIdx) : raw;
  const queryIdx = withoutFragment.indexOf('?');
  if (queryIdx < 0) return { base: withoutFragment, query: '', fragment };
  return {
    base: withoutFragment.slice(0, queryIdx),
    query: withoutFragment.slice(queryIdx + 1),
    fragment,
  };
}

/** 解析 query 串为保序键值对；模板保持原样，畸形转义回落为原文。 */
export function parseQueryPairs(query: string): Array<{ key: string; value: string }> {
  if (!query) return [];
  const out: Array<{ key: string; value: string }> = [];
  for (const seg of query.split('&')) {
    if (!seg) continue;
    const eq = seg.indexOf('=');
    const rawKey = eq >= 0 ? seg.slice(0, eq) : seg;
    const rawValue = eq >= 0 ? seg.slice(eq + 1) : '';
    out.push({ key: decodeQueryComponent(rawKey), value: decodeQueryComponent(rawValue) });
  }
  return out;
}

function decodeQueryComponent(raw: string): string {
  if (!raw.includes('%') && !raw.includes('+')) return raw;
  if (raw.includes('{{')) return raw;
  try {
    return decodeURIComponent(raw.replace(/\+/g, ' '));
  } catch {
    return raw;
  }
}

/** 仅编码会改变 query 结构的字符（& # + %），模板与其余字符保持可读原样。 */
export function encodeQueryComponent(raw: string): string {
  let out = '';
  let rest = raw;
  while (rest) {
    const match = rest.match(TEMPLATE_RE);
    if (!match || match.index === undefined) {
      out += escapeStructural(rest);
      break;
    }
    out += escapeStructural(rest.slice(0, match.index)) + match[0];
    rest = rest.slice(match.index + match[0].length);
  }
  return out;
}

function escapeStructural(s: string): string {
  return s.replace(/[&#+% ]/g, (ch) => STRUCTURAL_ESCAPES[ch]);
}

/** 按行顺序重建 URL：仅启用的行参与序列化，禁用行从 URL 中剔除。 */
export function withParamsQuery(
  raw: string,
  params: Array<{ key: string; value: string; enabled?: boolean }>,
): string {
  const { base, fragment } = splitUrlParts(raw);
  const pairs = params
    .filter((p) => p.enabled !== false && p.key !== '')
    .map((p) => `${encodeQueryComponent(p.key)}=${encodeQueryComponent(p.value)}`);
  if (pairs.length === 0) return base + fragment;
  return `${base}?${pairs.join('&')}${fragment}`;
}

const pairId = (key: string, value: string) => `${key}\u0000${value}`;

/**
 * URL query 变更 → Params 行。返回 null 表示 query 未变化（base/fragment 编辑不动表格）。
 * - 旧 URL 带 query：新 query 是权威——启用行按其重建；键不在新 query 的禁用行保留，
 *   同键禁用行被吸收（URL 已接管该键）。
 * - 旧 URL 无 query 且表格有启用行：视为存量"表格为主"状态，新键值对按 key+value
 *   去重后追加，不清空原有行。
 */
export function syncParamsFromUrl(
  oldUrl: string,
  newUrl: string,
  currentParams: Array<{ key: string; value: string; enabled?: boolean }>,
): Array<{ key: string; value: string; enabled: boolean }> | null {
  const oldParts = splitUrlParts(oldUrl);
  const newParts = splitUrlParts(newUrl);
  if (oldParts.query === newParts.query) return null;
  const newPairs = parseQueryPairs(newParts.query);
  const asRows = newPairs.map((p) => ({ key: p.key, value: p.value, enabled: true }));
  const existing = currentParams.map((p) => ({
    key: p.key,
    value: p.value,
    enabled: p.enabled !== false,
  }));

  if (oldParts.query === '' && existing.some((p) => p.enabled)) {
    const seen = new Set(existing.filter((p) => p.enabled).map((p) => pairId(p.key, p.value)));
    return [...existing, ...asRows.filter((p) => !seen.has(pairId(p.key, p.value)))];
  }

  const newKeys = new Set(newPairs.map((p) => p.key));
  const survivors = existing.filter((p) => !p.enabled && !newKeys.has(p.key));
  return [...asRows, ...survivors];
}

/** 批量编辑导出：每行 "key:value"，禁用行以 // 前缀标注。 */
export function formatBatchLines(
  items: Array<{ key: string; value: string; enabled?: boolean }>,
): string {
  return items
    .map((it) => `${it.enabled === false ? '//' : ''}${it.key}:${it.value}`)
    .join('\n');
}

/** 批量编辑导入：支持 "key:value" / "key=value"，// 前缀为禁用行，无分隔符的行丢弃。 */
export function parseBatchLines(text: string): Array<{ key: string; value: string; enabled: boolean }> {
  const out: Array<{ key: string; value: string; enabled: boolean }> = [];
  for (const line of text.split('\n')) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    const disabled = trimmed.startsWith('//');
    const body = disabled ? trimmed.slice(2) : trimmed;
    const sep = body.search(/[=:]/);
    if (sep <= 0) continue;
    const key = body.slice(0, sep).trim();
    if (!key) continue;
    out.push({ key, value: body.slice(sep + 1), enabled: !disabled });
  }
  return out;
}
