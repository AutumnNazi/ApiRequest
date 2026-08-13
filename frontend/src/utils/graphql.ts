export interface GraphqlOperationField {
  name: string;
  args?: string;
  kind: 'query' | 'mutation' | 'subscription';
  returnKind?: string;
}

interface GraphqlArgument {
  name: string;
  type: string;
}

interface GraphqlTypeRef {
  kind?: string;
  name?: string;
  ofType?: GraphqlTypeRef;
}

function typeRefToString(type: unknown): string {
  if (typeof type === 'string') return type;
  if (!type || typeof type !== 'object') return '';
  const ref = type as GraphqlTypeRef;
  if (ref.kind === 'NON_NULL') {
    const inner = typeRefToString(ref.ofType);
    return inner ? `${inner}!` : '';
  }
  if (ref.kind === 'LIST') {
    const inner = typeRefToString(ref.ofType);
    return inner ? `[${inner}]` : '';
  }
  return typeof ref.name === 'string' ? ref.name : '';
}

export function buildGraphqlOperation(field: GraphqlOperationField): { query: string; variables: string } {
  let args: GraphqlArgument[] = [];
  try {
    const parsed = JSON.parse(field.args ?? '[]') as unknown;
    if (Array.isArray(parsed)) {
      args = parsed.flatMap((arg): GraphqlArgument[] => {
        if (arg == null || typeof arg !== 'object') return [];
        const candidate = arg as { name?: unknown; type?: unknown };
        const type = typeRefToString(candidate.type);
        return typeof candidate.name === 'string' && candidate.name && type
          ? [{ name: candidate.name, type }]
          : [];
      });
    }
  } catch {
    args = [];
  }

  const vars = args.map((arg) => `$${arg.name}: ${arg.type}`).join(', ');
  const callArgs = args.map((arg) => `${arg.name}: $${arg.name}`).join(', ');
  const operation = `${field.kind} ${field.name}${vars ? `(${vars})` : ''}`;
  const call = `${field.name}${callArgs ? `(${callArgs})` : ''}`;
  const needsSelection = field.returnKind !== 'SCALAR' && field.returnKind !== 'ENUM';
  const query = needsSelection
    ? `${operation} {\n  ${call} {\n    # 在此选择返回字段\n  }\n}`
    : `${operation} {\n  ${call}\n}`;
  const variables = args.length > 0
    ? JSON.stringify(Object.fromEntries(args.map((arg) => [arg.name, null])), null, 2)
    : '';
  return { query, variables };
}
