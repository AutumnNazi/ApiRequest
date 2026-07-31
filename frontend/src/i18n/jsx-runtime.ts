import {
  Fragment,
  jsx as reactJsx,
  jsxs as reactJsxs,
  type JSX,
} from 'react/jsx-runtime';
import { translateExact } from './locale';

export { Fragment, type JSX };

const localizedAttributes = new Set(['title', 'placeholder', 'aria-label', 'alt']);

function localizeChild(value: unknown): unknown {
  if (typeof value === 'string') return translateExact(value);
  if (Array.isArray(value)) return value.map(localizeChild);
  return value;
}

export function localizeProps(props: unknown): unknown {
  if (!props || typeof props !== 'object') return props;
  const source = props as Record<string, unknown>;
  if (source['data-i18n-verbatim'] === true) return props;
  let next: Record<string, unknown> | undefined;
  const assign = (key: string, value: unknown) => {
    if (value === source[key]) return;
    next ??= { ...source };
    next[key] = value;
  };
  assign('children', localizeChild(source.children));
  for (const attribute of localizedAttributes) {
    if (typeof source[attribute] === 'string') assign(attribute, translateExact(source[attribute]));
  }
  return next ?? props;
}

export const jsx: typeof reactJsx = (type, props, key) => reactJsx(type, localizeProps(props), key);
export const jsxs: typeof reactJsxs = (type, props, key) => reactJsxs(type, localizeProps(props), key);
