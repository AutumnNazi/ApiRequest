// GraphQL query 的 schema 感知校验（docs/roadmap.md：GraphQL schema 断言）。
// 不引入 graphql-js parser（保持零运行时依赖、纯 Go 后端约束下的前端轻量实现）：
// 手写词法扫描 + 花括号配对，按内省 schema 逐字段路径校验。
// 覆盖高频错误：字段不存在、标量叶子上误加子选择、对象字段漏选择集、变量未声明。

export interface SchemaShape {
  queryType: string | null;
  mutationType: string | null | undefined;
  subscriptionType?: string | null;
  types: Record<string, { kind?: string; fields?: Record<string, { args?: unknown[]; type?: unknown }> }>;
}

export interface ValidationIssue {
  message: string;
  typeName: string;
  field?: string;
  line: number;
}

/** 把内省 schemaJson 归一化为按名索引的形状（types 数组 → map）。 */
export function normalizeSchema(raw: unknown): SchemaShape | null {
  if (!raw || typeof raw !== 'object') return null;
  const root = raw as {
    queryType?: { name?: string } | null;
    mutationType?: { name?: string } | null;
    subscriptionType?: { name?: string } | null;
    types?: Array<{ name?: string; kind?: string; fields?: unknown[] }>;
  };
  if (!Array.isArray(root.types)) return null;
  const types: SchemaShape['types'] = {};
  for (const t of root.types) {
    if (!t || typeof t.name !== 'string') continue;
    const fields: Record<string, { args?: unknown[]; type?: unknown }> = {};
    if (Array.isArray(t.fields)) {
      for (const f of t.fields) {
        if (f && typeof (f as { name?: unknown }).name === 'string') {
          fields[(f as { name: string }).name] = f as { args?: unknown[]; type?: unknown };
        }
      }
    }
    types[t.name] = { kind: t.kind, fields };
  }
  return {
    queryType: root.queryType?.name ?? null,
    mutationType: root.mutationType?.name ?? null,
    subscriptionType: root.subscriptionType?.name ?? null,
    types,
  };
}

interface Token {
  kind: 'word' | 'punct' | 'variable';
  value: string;
  line: number;
}

/** 极简词法：标识符/标点/变量（$x）三态，字符串与注释整体跳过。 */
function tokenize(src: string): Token[] {
  const out: Token[] = [];
  let i = 0;
  let line = 1;
  while (i < src.length) {
    const ch = src[i];
    if (ch === '\n') { line++; i++; continue; }
    if (ch === ' ' || ch === '\t' || ch === '\r') { i++; continue; }
    if (ch === '#') { while (i < src.length && src[i] !== '\n') i++; continue; }
    if (ch === '"') {
      // 三引号块字符串整体跳过
      if (src.slice(i, i + 3) === '"""') {
        i += 3;
        while (i < src.length && src.slice(i, i + 3) !== '"""') {
          if (src[i] === '\n') line++;
          i++;
        }
        i += 3;
        continue;
      }
      i++;
      while (i < src.length && src[i] !== '"') {
        if (src[i] === '\\') i++;
        if (src[i] === '\n') line++;
        i++;
      }
      i++;
      continue;
    }
    if (ch === '$') {
      let j = i + 1;
      while (j < src.length && /[\w]/.test(src[j])) j++;
      out.push({ kind: 'variable', value: src.slice(i, j), line });
      i = j;
      continue;
    }
    if (/[\w]/.test(ch)) {
      let j = i;
      while (j < src.length && /[\w]/.test(src[j])) j++;
      out.push({ kind: 'word', value: src.slice(i, j), line });
      i = j;
      continue;
    }
    out.push({ kind: 'punct', value: ch, line });
    i++;
  }
  return out;
}

function typeRefName(ref: unknown): string {
  let raw = '';
  if (typeof ref === 'string') {
    raw = ref; // 测试/简化 schema 直接给类型名（可带 [] ! 装饰）
  } else {
    // TypeRef {kind, name, ofType} 嵌套（NON_NULL/LIST 包装），取最内层命名
    let cur = ref;
    let guard = 0;
    while (cur && typeof cur === 'object' && guard++ < 16) {
      const r = cur as { kind?: string; name?: string; ofType?: unknown };
      if (r.name) { raw = r.name; break; }
      cur = r.ofType;
    }
  }
  // 剥掉 NonNull(!) 与 List([]) 装饰，取基础名
  return raw.replace(/[[\]]|!/g, '').trim();
}

function returnTypeString(ref: unknown): string {
  if (typeof ref === 'string') return ref;
  if (!ref || typeof ref !== 'object') return '';
  const r = ref as { kind?: string; name?: string; ofType?: unknown };
  if (r.kind === 'NON_NULL') return `${returnTypeString(r.ofType)}!`;
  if (r.kind === 'LIST') return `[${returnTypeString(r.ofType)}]`;
  return r.name ?? '';
}

/** 校验 query 文本对 schema 的字段路径合法性。 */
export function validateQueryAgainstSchema(query: string, schema: SchemaShape): ValidationIssue[] {
  const tokens = tokenize(query);
  const issues: ValidationIssue[] = [];
  const push = (message: string, typeName: string, field: string | undefined, line: number) =>
    issues.push({ message, typeName, field, line });

  // 1. 括号配对（语法层快速失败）
  let braceDepth = 0;
  let parenDepth = 0;
  for (const t of tokens) {
    if (t.kind !== 'punct') continue;
    if (t.value === '{' || t.value === '[') braceDepth++;
    if (t.value === '}' || t.value === ']') braceDepth--;
    if (t.value === '(') parenDepth++;
    if (t.value === ')') parenDepth--;
    if (braceDepth < 0) {
      push('语法错误：多余的 "}"', '—', undefined, t.line);
      return issues;
    }
    if (parenDepth < 0) {
      push('语法错误：多余的 ")"', '—', undefined, t.line);
      return issues;
    }
  }
  if (braceDepth !== 0 || parenDepth !== 0) {
    push(`语法错误：括号不配对（花括号 ${braceDepth}、圆括号 ${parenDepth}）`, '—', undefined, tokens.at(-1)?.line ?? 1);
    return issues;
  }

  // 2. 解析 operation 头部（kind? name? 变量声明? 指令?）
  let idx = 0;
  let rootKind: 'query' | 'mutation' | 'subscription' = 'query';
  const declaredVars = new Set<string>();
  const head = tokens[0];
  if (head && head.kind === 'word' && ['query', 'mutation', 'subscription'].includes(head.value)) {
    rootKind = head.value as typeof rootKind;
    idx = 1;
  }
  if (tokens[idx]?.kind === 'word' && tokens[idx]?.value !== '{') idx++;
  if (tokens[idx]?.kind === 'punct' && tokens[idx]?.value === '(') {
    let close = idx;
    while (close < tokens.length && !(tokens[close].kind === 'punct' && tokens[close].value === ')')) {
      if (tokens[close].kind === 'variable') declaredVars.add(tokens[close].value);
      close++;
    }
    idx = close + 1;
  }
  while (tokens[idx]?.kind === 'punct' && tokens[idx]?.value === '@') idx += 2;

  const rootType =
    rootKind === 'query' ? schema.queryType
      : rootKind === 'mutation' ? schema.mutationType ?? null
        : schema.subscriptionType ?? null;
  // 根类型缺失时回退 Query 根：让字段级错误（如 "renameUser 不存在"）比笼统的
  // "未定义根类型" 更有操作性；Query 也没有才放弃
  const effectiveRoot = rootType && schema.types[rootType] ? rootType : schema.queryType;
  if (!effectiveRoot || !schema.types[effectiveRoot]) {
    push(`schema 未定义 ${rootKind} 根类型`, '—', undefined, tokens[idx]?.line ?? 1);
    return issues;
  }

  // 3. 递归遍历 selection set
  const walkSet = (start: number, end: number, typeName: string): number => {
    const type = schema.types[typeName];
    if (!type) return end; // 未知类型（union 成员缺失等）：跳过内部校验
    let i = start + 1; // 跳过 '{'
    while (i < end) {
      const t = tokens[i];
      if (t.kind === 'word' && t.value === 'on') {
        // inline fragment：... on Type { … } —— 后随类型名重定向
        const condType = tokens[i + 1]?.kind === 'word' ? tokens[i + 1].value : null;
        const setIdx = tokens.findIndex((tk, k) => k > i && tk.kind === 'punct' && tk.value === '{');
        if (setIdx > 0 && setIdx < end && condType) {
          i = walkSet(setIdx, end, condType);
          continue;
        }
        i += 2;
        continue;
      }
      if (t.kind !== 'word') { i++; continue; }

      // 统一定位字段边界：参数列表、指令、可选子选择集
      const name = t.value;
      let j = i + 1;
      let argD = 0;
      let selStart = -1;
      let selEnd = -1;
      while (j < end) {
        const tk = tokens[j];
        if (tk.kind === 'punct' && tk.value === '(') { argD++; j++; continue; }
        if (tk.kind === 'punct' && tk.value === ')') { argD--; j++; continue; }
        if (argD > 0) {
          if (tk.kind === 'variable' && !declaredVars.has(tk.value)) {
            push(`变量 ${tk.value} 未在操作头声明`, typeName, name, tk.line);
          }
          j++;
          continue;
        }
        if (tk.kind === 'punct' && tk.value === '@') { j += 2; continue; }
        if (tk.kind === 'punct' && tk.value === '{') {
          selStart = j;
          let d = 0;
          let k = j;
          for (; k < end; k++) {
            if (tokens[k].kind === 'punct' && tokens[k].value === '{') d++;
            if (tokens[k].kind === 'punct' && tokens[k].value === '}') { d--; if (d === 0) break; }
          }
          selEnd = k;
          j = k + 1;
          continue;
        }
        break; // 到达下一个字段 / 片段 / 闭合
      }
      const next = j;

      if (name.startsWith('__')) { i = next; continue; } // 内省字段整体放行
      const fieldDef = type.fields?.[name];
      if (!fieldDef) {
        push(`字段 "${name}" 不存在于类型 ${typeName}`, typeName, name, t.line);
        i = next;
        continue;
      }
      const retName = typeRefName(fieldDef.type);
      const retType = schema.types[retName];
      const isLeaf = !retType || retType.kind === 'SCALAR' || retType.kind === 'ENUM';
      if (selStart > 0) {
        if (isLeaf) {
          push(`字段 "${name}"（${returnTypeString(fieldDef.type)}）是标量/枚举，不能有子选择集`, typeName, name, t.line);
        } else if (retName) {
          walkSet(selStart, selEnd, retName);
        }
      } else if (!isLeaf) {
        push(`字段 "${name}"（${returnTypeString(fieldDef.type)}）是对象/接口/联合类型，需要子选择集 { … }`, typeName, name, t.line);
      }
      i = next;
    }
    return end;
  };

  const firstBrace = tokens.findIndex((t) => t.kind === 'punct' && t.value === '{');
  if (firstBrace < 0) {
    push('语法错误：缺少选择集', '—', undefined, tokens.at(-1)?.line ?? 1);
    return issues;
  }
  const lastClose = tokens.reduce((acc, t, k) => (t.kind === 'punct' && t.value === '}' ? k : acc), -1);
  walkSet(firstBrace, lastClose < 0 ? tokens.length : lastClose + 1, effectiveRoot);
  return issues;
}
