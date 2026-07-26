// ipc 层：Wails 绑定的 typed wrapper。组件层禁止直接 import wailsjs（docs/frontend.md §3）。
import * as RequestApi from '../../wailsjs/go/binding/RequestApi';
import * as NodeApi from '../../wailsjs/go/binding/NodeApi';
import * as HistoryApi from '../../wailsjs/go/binding/HistoryApi';
import { EventsOn } from '../../wailsjs/runtime/runtime';
import { model } from '../../wailsjs/go/models';
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
