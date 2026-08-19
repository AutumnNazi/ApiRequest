import { render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import AuthEditor from './AuthEditor';

vi.mock('../ipc', () => ({
  getOAuth2Token: vi.fn(),
  clearOAuth2Token: vi.fn(),
  toAppError: vi.fn(),
}));

describe('AuthEditor credential fields', () => {
  it('masks the OAuth 1.0 token classified as sensitive by persistence', () => {
    render(
      <AuthEditor
        auth={{ type: 'oauth1', params: { token: 'oauth-token' } }}
        onChange={() => undefined}
      />,
    );

    expect(screen.getByDisplayValue('oauth-token')).toHaveAttribute('type', 'password');
  });
});
