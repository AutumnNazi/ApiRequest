// JSON 文本的格式化 / 压缩工具。
//
// 为何不用 JSON.parse + JSON.stringify：JS 的 Number 是 IEEE-754 双精度，
// 超过 2^53-1 的整数（雪花 ID、纳秒时间戳等）在解析时就已静默丢精度，
// 再序列化回来就变成另一个数：1547040075470688258 -> 1547040075470688300。
//
// 这里改为词法扫描：用 JSON.parse 只做合法性校验，输出则逐 token 回填
// 原始字面量（字符串与数字都按原文切片），因此除空白外与原文逐字一致。

type Token =
  | { kind: 'open'; text: string }
  | { kind: 'close'; text: string }
  | { kind: 'colon' }
  | { kind: 'comma' }
  | { kind: 'value'; text: string };

const WHITESPACE = new Set([' ', '\t', '\n', '\r']);
const PUNCTUATION = new Set(['{', '}', '[', ']', ':', ',']);

function tokenize(text: string): Token[] {
  const tokens: Token[] = [];
  let i = 0;
  while (i < text.length) {
    const ch = text[i];
    if (WHITESPACE.has(ch)) {
      i++;
      continue;
    }
    if (ch === '{' || ch === '[' || ch === '}' || ch === ']') {
      tokens.push({ kind: ch === '{' || ch === '[' ? 'open' : 'close', text: ch });
      i++;
      continue;
    }
    if (ch === ':') {
      tokens.push({ kind: 'colon' });
      i++;
      continue;
    }
    if (ch === ',') {
      tokens.push({ kind: 'comma' });
      i++;
      continue;
    }
    if (ch === '"') {
      const start = i;
      i++;
      while (i < text.length) {
        if (text[i] === '\\') {
          i += 2;
          continue;
        }
        if (text[i] === '"') {
          i++;
          break;
        }
        i++;
      }
      tokens.push({ kind: 'value', text: text.slice(start, i) });
      continue;
    }
    // 数字与 true / false / null：原样切片，不做数值转换
    const start = i;
    while (i < text.length && !WHITESPACE.has(text[i]) && !PUNCTUATION.has(text[i])) i++;
    tokens.push({ kind: 'value', text: text.slice(start, i) });
  }
  return tokens;
}

function emit(tokens: Token[], indentUnit: string | null): string {
  const compact = indentUnit === null;
  const unit = indentUnit ?? '';
  const newline = compact ? '' : '\n';
  const pad = (depth: number) => (compact ? '' : unit.repeat(depth));
  let out = '';
  let depth = 0;
  for (let k = 0; k < tokens.length; k++) {
    const token = tokens[k];
    if (token.kind === 'comma') {
      out += ',' + newline + pad(depth);
    } else if (token.kind === 'colon') {
      out += compact ? ':' : ': ';
    } else if (token.kind === 'open') {
      depth++;
      const empty = tokens[k + 1]?.kind === 'close';
      out += token.text + (empty ? '' : newline + pad(depth));
    } else if (token.kind === 'close') {
      depth--;
      const empty = tokens[k - 1]?.kind === 'open';
      out += (empty ? '' : newline + pad(depth)) + token.text;
    } else {
      out += token.text;
    }
  }
  return out;
}

// 按指定缩进美化 JSON；文本非法时抛出 SyntaxError（与 JSON.parse 一致）。
export function formatJsonText(text: string, indent = 2): string {
  JSON.parse(text); // 仅用于校验，不取值，避免大整数丢精度
  return emit(tokenize(text), ' '.repeat(indent));
}

// 去掉所有可省略空白；文本非法时抛出 SyntaxError。
export function compressJsonText(text: string): string {
  JSON.parse(text);
  return emit(tokenize(text), null);
}

// 美化 JSON，非法文本返回 null（供展示层回退原文）。
export function tryFormatJsonText(text: string, indent = 2): string | null {
  try {
    return formatJsonText(text, indent);
  } catch {
    return null;
  }
}
