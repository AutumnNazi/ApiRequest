const SCHEME_RE = /^[a-z][a-z\d+.-]*:\/\//i;
const LEADING_VARIABLE_RE = /^\{\{[^}]+\}\}/;

export function ensureHttpScheme(value: string): string {
  const trimmed = value.trim();
  if (!trimmed || SCHEME_RE.test(trimmed) || LEADING_VARIABLE_RE.test(trimmed)) return trimmed;
  return `http://${trimmed}`;
}

export function sanitizeRecentTarget(value: string): string {
  const trimmed = value.trim();
  if (!trimmed || !SCHEME_RE.test(trimmed)) return trimmed;
  try {
    const parsed = new URL(trimmed);
    // Dropping any of these parts can silently turn a valid endpoint into a
    // different target. Do not persist it when it cannot be recalled safely.
    if (parsed.username || parsed.password || parsed.search || parsed.hash) return '';
    return parsed.toString().replace(/\/$/, parsed.pathname === '/' ? '' : '/');
  } catch {
    return '';
  }
}

export function parseHeaderLine(value: string): { key: string; value: string } | null {
  const separator = value.indexOf(':');
  if (separator <= 0) return null;
  const key = value.slice(0, separator).trim();
  if (!key) return null;
  return { key, value: value.slice(separator + 1).trim() };
}

export function splitRequestAuthHeader(header?: { key: string; value: string }): {
  header?: { key: string; value: string };
  auth?: { type: string; params: Record<string, string> };
} {
  if (!header || !/^authorization$/i.test(header.key)) return header ? { header } : {};
  const bearer = header.value.match(/^Bearer\s+(.+)$/i);
  if (bearer) return { auth: { type: 'bearer', params: { token: bearer[1] } } };
  return {
    auth: {
      type: 'apikey',
      params: { key: header.key, value: header.value, in: 'header' },
    },
  };
}
