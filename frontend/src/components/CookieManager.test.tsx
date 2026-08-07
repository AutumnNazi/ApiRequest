import { describe, expect, it } from 'vitest';
import { normalizeImportedCookies } from './CookieManager';

describe('normalizeImportedCookies', () => {
  it('normalizes legacy fields and preserves domain scope', () => {
    const [cookie] = normalizeImportedCookies([{
      name: 'session', value: 'secret', domain: '.example.test', sameSite: 'Lax', secure: true,
    }]);
    expect(cookie).toMatchObject({
      name: 'session', value: 'secret', domain: '.example.test', path: '/',
      sameSite: 'lax', secure: true, hostOnly: false, expires: 0,
    });
  });

  it('rejects an invalid SameSite value before starting the batch', () => {
    expect(() => normalizeImportedCookies([{
      name: 'session', domain: 'example.test', sameSite: 'sometimes',
    }])).toThrow('SameSite');
  });
});
