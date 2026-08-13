import { describe, expect, it } from 'vitest';
import { buildGraphqlOperation } from './graphql';

describe('buildGraphqlOperation', () => {
  it('omits a selection set for scalar and enum fields', () => {
    expect(buildGraphqlOperation({ name: 'health', kind: 'query', returnKind: 'SCALAR' }).query).toBe(
      'query health {\n  health\n}',
    );
    expect(buildGraphqlOperation({ name: 'status', kind: 'query', returnKind: 'ENUM' }).query).toBe(
      'query status {\n  status\n}',
    );
  });

  it('keeps a selection placeholder for object fields and builds variables', () => {
    const built = buildGraphqlOperation({
      name: 'user',
      kind: 'query',
      returnKind: 'OBJECT',
      args: '[{"name":"id","type":"ID!"}]',
    });
    expect(built.query).toContain('user(id: $id) {\n    # 在此选择返回字段\n  }');
    expect(built.variables).toBe('{\n  "id": null\n}');
  });

  it('accepts the nested TypeRef shape returned by introspection', () => {
    const built = buildGraphqlOperation({
      name: 'users',
      kind: 'query',
      returnKind: 'OBJECT',
      args: JSON.stringify([
        {
          name: 'ids',
          type: {
            kind: 'NON_NULL',
            ofType: {
              kind: 'LIST',
              ofType: {
                kind: 'NON_NULL',
                ofType: { kind: 'SCALAR', name: 'ID' },
              },
            },
          },
        },
      ]),
    });
    expect(built.query).toContain('query users($ids: [ID!]!)');
    expect(built.query).toContain('users(ids: $ids)');
  });
});
