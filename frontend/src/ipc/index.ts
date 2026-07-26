// ipc 层：Wails 绑定的 typed wrapper。组件层禁止直接 import wailsjs（docs/frontend.md §3）。
import * as RequestApi from '../../wailsjs/go/binding/RequestApi';
import * as NodeApi from '../../wailsjs/go/binding/NodeApi';
import * as HistoryApi from '../../wailsjs/go/binding/HistoryApi';
import * as EnvApi from '../../wailsjs/go/binding/EnvApi';
import * as CookieApi from '../../wailsjs/go/binding/CookieApi';
import * as ConvertApi from '../../wailsjs/go/binding/ConvertApi';
import { EventsOn } from '../../wailsjs/runtime/runtime';
import { model, convert, codegen } from '../../wailsjs/go/models';
import { call } from './error';

export type HttpRequest = model.HttpRequest;
export type SendContext = model.SendContext;
export type ResponseResult = model.ResponseResult;
export type Node = model.Node;
export type Workspace = model.Workspace;
export type HistoryItem = model.HistoryItem;
export type HistoryQuery = model.HistoryQuery;
export type KV = model.KV;
export type Body = model.Body;
export type Timing = model.Timing;
export type Environment = model.Environment;
export type Variable = model.Variable;
export type TestResult = model.TestResult;
export type Cookie = model.Cookie;
export type Auth = model.Auth;
export type ImportResult = convert.ImportResult;
export type CodegenTarget = codegen.Target;

export { toAppError } from './error';
export type { AppError, ErrorKind } from './error';

// ── 请求执行 ──

export const sendRequest = (sendId: string, req: HttpRequest, ctx: SendContext) =>
  call(() => RequestApi.SendRequest(sendId, req, ctx));

export const cancelRequest = (sendId: string) => call(() => RequestApi.CancelRequest(sendId));

// ── 集合树 ──

export const getDefaultWorkspace = () => call(() => NodeApi.GetDefaultWorkspace());
export const listNodes = (workspaceId: string) => call(() => NodeApi.ListNodes(workspaceId));
export const upsertNode = (node: Node) => call(() => NodeApi.UpsertNode(node));
export const deleteNode = (nodeId: string) => call(() => NodeApi.DeleteNode(nodeId));
export const moveNode = (nodeId: string, newParentId: string, sortOrder: number) =>
  call(() => NodeApi.MoveNode(nodeId, newParentId, sortOrder));

// ── 历史 ──

export const listHistory = (workspaceId: string, q: Partial<HistoryQuery> = {}) =>
  call(() => HistoryApi.ListHistory(workspaceId, model.HistoryQuery.createFrom(q)));
export const clearHistory = (workspaceId: string) => call(() => HistoryApi.ClearHistory(workspaceId));

// ── 环境与全局变量 ──

export const listEnvironments = (workspaceId: string) =>
  call(() => EnvApi.ListEnvironments(workspaceId));
export const upsertEnvironment = (env: Environment) => call(() => EnvApi.UpsertEnvironment(env));
export const deleteEnvironment = (envId: string) => call(() => EnvApi.DeleteEnvironment(envId));
export const setActiveEnvironment = (workspaceId: string, envId: string) =>
  call(() => EnvApi.SetActiveEnvironment(workspaceId, envId));
export const getGlobalVariables = (workspaceId: string) =>
  call(() => EnvApi.GetGlobalVariables(workspaceId));
export const setGlobalVariables = (workspaceId: string, vars: Variable[]) =>
  call(() => EnvApi.SetGlobalVariables(workspaceId, vars));

// ── Cookie ──

export const listCookies = (domain = '') => call(() => CookieApi.ListCookies(domain));
export const upsertCookie = (c: Cookie) => call(() => CookieApi.UpsertCookie(c));
export const deleteCookie = (domain: string, path: string, name: string) =>
  call(() => CookieApi.DeleteCookie(domain, path, name));
export const clearCookies = (domain = '') => call(() => CookieApi.ClearCookies(domain));

// ── 导入导出与代码生成 ──

export const importPreview = (format: string, payload: string) =>
  call(() => ConvertApi.ImportPreview(format, payload));
export const importCommit = (workspaceId: string, res: ImportResult) =>
  call(() => ConvertApi.ImportCommit(workspaceId, res));
export const exportData = (collectionId: string, format: string) =>
  call(() => ConvertApi.ExportData(collectionId, format));
export const codegenTargets = () => call(() => ConvertApi.CodegenTargets());
export const generateCode = (target: string, req: HttpRequest) =>
  call(() => ConvertApi.GenerateCode(target, req));

// ── 事件 ──

export interface RequestProgress {
  sendId: string;
  phase: 'sending' | 'done';
}

/** 订阅请求进度事件；返回取消订阅函数 */
export function onRequestProgress(handler: (p: RequestProgress) => void): () => void {
  return EventsOn('request:progress', handler);
}

// ── 工厂 ──

export function newDefaultRequest(): HttpRequest {
  return model.HttpRequest.createFrom({
    method: 'GET',
    url: '',
    params: [],
    headers: [],
    body: { kind: 'none' },
    auth: { type: 'none' },
    settings: { timeoutMs: 30000, followRedirects: true, maxRedirects: 10, verifyTls: true },
  });
}
