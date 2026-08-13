import { describe, expect, it } from 'vitest';
import { collectVarRefs } from './varRefs';

describe('collectVarRefs', () => {
  it('collects references across url, params, headers, body, auth', () => {
    const refs = collectVarRefs(
      {
        url: 'https://{{host}}/{{path}}',
        params: [{ key: 'limit', value: '{{limit}}', enabled: true, description: '' }],
        headers: [{ key: 'X-Token', value: '{{token}}', enabled: true, description: '' }],
        body: { kind: 'raw', text: '{"id": "{{userId}}"}', query: '', variables: '' },
        auth: { params: { token: '{{authToken}}' } },
      } as unknown as Parameters<typeof collectVarRefs>[0],
    );
    expect(refs.sort()).toEqual(['authToken', 'host', 'limit', 'path', 'token', 'userId']);
  });

  it('skips {{$dynamic}} variables and deduplicates names', () => {
    const refs = collectVarRefs({
      url: '{{a}}/{{a}}/{{$guid}}',
      body: { kind: 'raw', text: '{{b}} {{$timestamp}}', query: '', variables: '' },
    } as unknown as Parameters<typeof collectVarRefs>[0]);
    expect(refs.sort()).toEqual(['a', 'b']);
  });

  it('returns empty array when no references exist', () => {
    expect(
      collectVarRefs({
        url: 'https://example.com',
        body: {},
      } as unknown as Parameters<typeof collectVarRefs>[0]),
    ).toEqual([]);
  });

  it('ignores references in disabled params, headers, and body items', () => {
    expect(
      collectVarRefs({
        params: [{ key: 'disabled', value: '{{param}}', enabled: false, description: '' }],
        headers: [{ key: 'X-Disabled', value: '{{header}}', enabled: false, description: '' }],
        body: {
          kind: 'formdata',
          items: [{ key: 'file', type: 'text', value: '{{body}}', path: '', enabled: false }],
        },
      } as unknown as Parameters<typeof collectVarRefs>[0]),
    ).toEqual([]);
  });
});
