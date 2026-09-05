import { describe, expect, it } from 'vitest';
import { validateQueryAgainstSchema, type SchemaShape } from './graphqlValidate';

// 最小但结构完整的 schema：Query.user(id)→User{name, email, posts}，Post{title, likes(Int)}
const schema: SchemaShape = {
  queryType: 'Query',
  mutationType: null,
  types: {
    Query: {
      kind: 'OBJECT',
      fields: {
        user: { args: [{ name: 'id', type: 'ID!' }], type: 'User' },
      },
    },
    User: {
      kind: 'OBJECT',
      fields: {
        name: { args: [], type: 'String' },
        email: { args: [], type: 'String' },
        posts: { args: [], type: '[Post]' },
      },
    },
    Post: {
      kind: 'OBJECT',
      fields: {
        title: { args: [], type: 'String' },
        likes: { args: [], type: 'Int' },
      },
    },
    String: { kind: 'SCALAR' },
    Int: { kind: 'SCALAR' },
    ID: { kind: 'SCALAR' },
  },
};

describe('validateQueryAgainstSchema', () => {
  it('accepts a valid nested query with variables', () => {
    const issues = validateQueryAgainstSchema(
      'query GetUser($id: ID!) { user(id: $id) { name posts { title likes } } }',
      schema,
    );
    expect(issues).toEqual([]);
  });

  it('reports unknown fields with the enclosing type and line context', () => {
    const issues = validateQueryAgainstSchema(
      'query { user(id: "1") { name typo } }',
      schema,
    );
    expect(issues).toHaveLength(1);
    expect(issues[0].message).toContain('typo');
    expect(issues[0].typeName).toBe('User');
  });

  it('reports unknown root operations', () => {
    const issues = validateQueryAgainstSchema('query { nobody(id: "1") { name } }', schema);
    expect(issues).toHaveLength(1);
    expect(issues[0].message).toContain('nobody');
    expect(issues[0].typeName).toBe('Query');
  });

  it('reports selection sets on scalar leaves', () => {
    const issues = validateQueryAgainstSchema(
      'query { user(id: "1") { name { inner } } }',
      schema,
    );
    expect(issues).toHaveLength(1);
    expect(issues[0].message).toMatch(/scalar|leaf|子选择/i);
  });

  it('reports missing selection sets on object fields', () => {
    const issues = validateQueryAgainstSchema('query { user(id: "1") }', schema);
    expect(issues).toHaveLength(1);
    expect(issues[0].message).toMatch(/selection|子选择|选择集/i);
  });

  it('reports undefined variables used in arguments', () => {
    const issues = validateQueryAgainstSchema(
      'query { user(id: $missing) { name } }',
      schema,
    );
    expect(issues.some((i) => i.message.includes('$missing'))).toBe(true);
  });

  it('skips introspection fields', () => {
    const issues = validateQueryAgainstSchema(
      'query { __schema { queryType { name } } }',
      schema,
    );
    expect(issues).toEqual([]);
  });

  it('tolerates inline fragments and directives', () => {
    const issues = validateQueryAgainstSchema(
      'query { user(id: "1") { name posts { title @include(if: true) } } }',
      schema,
    );
    expect(issues).toEqual([]);
  });

  it('returns a syntax issue for unparseable queries instead of throwing', () => {
    const issues = validateQueryAgainstSchema('query { user(id: "1" { name } }', schema);
    expect(issues.length).toBeGreaterThan(0);
    expect(issues[0].message).toMatch(/语法|syntax|parse|括号/i);
  });

  it('reports mutations against the mutation root', () => {
    const issues = validateQueryAgainstSchema('mutation { renameUser(id: "1") }', schema);
    expect(issues[0].message).toContain('renameUser');
  });

  it('handles list wrappers in return types', () => {
    const issues = validateQueryAgainstSchema(
      'query { user(id: "1") { posts { title } } }',
      schema,
    );
    expect(issues).toEqual([]);
  });
});
