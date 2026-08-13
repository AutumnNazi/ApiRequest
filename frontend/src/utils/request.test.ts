import { describe, expect, it } from 'vitest';
import { ensureHttpScheme, parseHeaderLine, sanitizeRecentTarget, splitRequestAuthHeader } from './request';
import { useRecentTargets } from '../hooks/useRecentTargets';
import { renderHook } from '@testing-library/react';

describe('ensureHttpScheme', () => {
  it('adds http to a plain host', () => {
    expect(ensureHttpScheme(' api.example.com/users ')).toBe('http://api.example.com/users');
  });

  it('preserves explicit schemes and variable-backed base URLs', () => {
    expect(ensureHttpScheme('https://api.example.com')).toBe('https://api.example.com');
    expect(ensureHttpScheme('{{baseUrl}}/users')).toBe('{{baseUrl}}/users');
    expect(ensureHttpScheme('  {{ baseUrl }}/users  ')).toBe('{{ baseUrl }}/users');
  });
});

describe('sanitizeRecentTarget', () => {
  it('rejects URLs whose credentials, query, or fragment cannot be stored safely without changing meaning', () => {
    expect(sanitizeRecentTarget('wss://alice:secret@example.com/socket?token=abc#debug')).toBe('');
    expect(sanitizeRecentTarget('https://example.com/graphql?tenant=acme')).toBe('');
  });

  it('keeps gRPC host targets intact', () => {
    expect(sanitizeRecentTarget('localhost:50051')).toBe('localhost:50051');
  });
});

describe('useRecentTargets migration', () => {
  it('rewrites legacy sensitive URLs in storage when they are read', () => {
    localStorage.setItem(
      'protocol:recent:test',
      JSON.stringify(['wss://alice:secret@example.com/socket?token=abc#debug']),
    );

    const { result } = renderHook(() => useRecentTargets('protocol:recent:test'));
    expect(result.current.recents).toEqual([]);
    expect(localStorage.getItem('protocol:recent:test')).toBe('[]');
  });
});

describe('parseHeaderLine', () => {
  it('preserves an introspection authorization header for generated requests', () => {
    expect(parseHeaderLine('Authorization: Bearer secret')).toEqual({
      key: 'Authorization',
      value: 'Bearer secret',
    });
  });

  it('rejects malformed header lines', () => {
    expect(parseHeaderLine('Bearer secret')).toBeNull();
    expect(parseHeaderLine(': value')).toBeNull();
  });
});

describe('splitRequestAuthHeader', () => {
  it('routes bearer credentials through the secure auth model', () => {
    expect(splitRequestAuthHeader({ key: 'Authorization', value: 'Bearer secret' })).toEqual({
      auth: { type: 'bearer', params: { token: 'secret' } },
    });
  });

  it('routes any other Authorization scheme through the Vault-backed API key auth model', () => {
    expect(splitRequestAuthHeader({ key: 'Authorization', value: 'Basic dXNlcjpwYXNz' })).toEqual({
      auth: {
        type: 'apikey',
        params: { key: 'Authorization', value: 'Basic dXNlcjpwYXNz', in: 'header' },
      },
    });
  });

  it('keeps non-bearer custom headers as headers', () => {
    const header = { key: 'X-Custom-Token', value: 'secret' };
    expect(splitRequestAuthHeader(header)).toEqual({ header });
  });
});
