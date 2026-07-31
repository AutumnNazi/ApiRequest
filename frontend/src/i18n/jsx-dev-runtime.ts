import {
  Fragment,
  jsxDEV as reactJsxDEV,
  type JSX,
} from 'react/jsx-dev-runtime';
import { localizeProps } from './jsx-runtime';

export { Fragment, type JSX };

export const jsxDEV: typeof reactJsxDEV = (type, props, key, isStaticChildren, source, self) =>
  reactJsxDEV(type, localizeProps(props), key, isStaticChildren, source, self);
