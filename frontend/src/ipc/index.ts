// ipc 层：Wails 绑定的 typed wrapper。组件层禁止直接 import wailsjs（docs/frontend.md §3）。
import * as RequestApi from '../../wailsjs/go/binding/RequestApi';
import * as NodeApi from '../../wailsjs/go/binding/NodeApi';
import * as HistoryApi from '../../wailsjs/go/binding/HistoryApi';
import * as EnvApi from '../../wailsjs/go/binding/EnvApi';
import * as CookieApi from '../../wailsjs/go/binding/CookieApi';
import * as ConvertApi from '../../wailsjs/go/binding/ConvertApi';
import * as RunnerApi from '../../wailsjs/go/binding/RunnerApi';
import * as ExampleApi from '../../wailsjs/go/binding/ExampleApi';
import * as MockApi from '../../wailsjs/go/binding/MockApi';
import * as ProtocolApi from '../../wailsjs/go/binding/ProtocolApi';
import * as OAuth2Api from '../../wailsjs/go/binding/OAuth2Api';
import * as SettingsApi from '../../wailsjs/go/binding/SettingsApi';
import { EventsOn } from '../../wailsjs/runtime/runtime';
import { model, convert, codegen, runner, mock, protocol, binding, httpengine } from '../../wailsjs/go/models';
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
export type RunnerOptions = runner.Options;
export type RunnerReport = runner.Report;
export type Example = model.Example;
export type MockOptions = mock.Options;
export type MockStatus = binding.MockStatus;
export type SessionConfig = protocol.SessionConfig;

// InboundMsg 仅经事件推送，Wails 不生成其类型——与 backend/protocol.InboundMsg 保持一致
export interface InboundMsg {
  sessionId: string;
  protocol: string;
  direction: 'in' | 'out' | 'system';
  kind: 'text' | 'binary' | 'open' | 'close' | 'error' | 'event';
  data: string;
  event?: string;
  ts: number;
}

export { toAppError } from './error';
export type { AppError, ErrorKind } from './error';

// ── 请求执行 ──

export const sendRequest = (sendId: string, req: HttpRequest, ctx: SendContext) =>
  call(() => RequestApi.SendRequest(sendId, req, ctx));

export const cancelRequest = (sendId: string) => call(() => RequestApi.CancelRequest(sendId));

/** 按引用拉取大响应体全文 */
export const getResponseBlob = (blobRef: string) =>
  call(() => RequestApi.GetResponseBlob(blobRef));

// ── 集合树 ──

export const getDefaultWorkspace = () => call(() => NodeApi.GetDefaultWorkspace());
export const listWorkspaces = () => call(() => NodeApi.ListWorkspaces());
export const createWorkspace = (name: string) => call(() => NodeApi.CreateWorkspace(name));
export const renameWorkspace = (id: string, name: string) =>
  call(() => NodeApi.RenameWorkspace(id, name));
export const deleteWorkspace = (id: string) => call(() => NodeApi.DeleteWorkspace(id));
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
export const exportMirror = (collectionId: string, dir: string) =>
  call(() => ConvertApi.ExportMirror(collectionId, dir));
export const importMirror = (workspaceId: string, dir: string) =>
  call(() => ConvertApi.ImportMirror(workspaceId, dir));

// ── Runner ──

export const runCollection = (
  runId: string,
  workspaceId: string,
  collectionId: string,
  opts: Partial<RunnerOptions>,
) => call(() => RunnerApi.RunCollection(runId, workspaceId, collectionId, runner.Options.createFrom(opts)));
export const cancelRun = (runId: string) => call(() => RunnerApi.CancelRun(runId));
export const exportReport = (runId: string) => call(() => RunnerApi.ExportReport(runId));

// ── Example / Mock ──

export const listExamples = (nodeId: string) => call(() => ExampleApi.ListExamples(nodeId));
export const upsertExample = (e: Example) => call(() => ExampleApi.UpsertExample(e));
export const deleteExample = (exampleId: string) => call(() => ExampleApi.DeleteExample(exampleId));
export const startMockServer = (collectionId: string, opts: Partial<MockOptions> = {}) =>
  call(() => MockApi.StartMockServer(collectionId, mock.Options.createFrom(opts)));
export const stopMockServer = (collectionId: string) =>
  call(() => MockApi.StopMockServer(collectionId));
export const runningMocks = () => call(() => MockApi.RunningMocks());

// ── 协议会话（WS/SSE）──

export const openSession = (sessionId: string, cfg: Partial<SessionConfig>) =>
  call(() => ProtocolApi.OpenSession(sessionId, protocol.SessionConfig.createFrom(cfg)));
export const sendSessionMessage = (sessionId: string, data: string) =>
  call(() => ProtocolApi.SendMessage(sessionId, data));
export const closeSession = (sessionId: string) => call(() => ProtocolApi.CloseSession(sessionId));

// ── OAuth 2.0 ──

export const getOAuth2Token = (params: Record<string, string>) =>
  call(() => OAuth2Api.GetOAuth2Token(params));
export const clearOAuth2Token = (params: Record<string, string>) =>
  call(() => OAuth2Api.ClearOAuth2Token(params));

// ── 应用设置 ──

export interface ProxySettings {
  mode: 'system' | 'manual' | 'none';
  url?: string;
}

export const getProxySettings = () =>
  call(() => SettingsApi.GetProxySettings()) as Promise<ProxySettings>;
export const setProxySettings = (p: ProxySettings) =>
  call(() => SettingsApi.SetProxySettings(binding.ProxySettings.createFrom(p)));

export interface TLSSettings {
  caCertPath?: string;
  clientCertPath?: string;
  clientKeyPath?: string;
}

export const getTLSSettings = () =>
  call(() => SettingsApi.GetTLSSettings()) as Promise<TLSSettings>;
export const setTLSSettings = (s: TLSSettings) =>
  call(() => SettingsApi.SetTLSSettings(httpengine.TLSSettings.createFrom(s)));

// ── 事件 ──

export interface RequestProgress {
  sendId: string;
  phase: 'sending' | 'done';
}

/** 订阅请求进度事件；返回取消订阅函数 */
export function onRequestProgress(handler: (p: RequestProgress) => void): () => void {
  return EventsOn('request:progress', handler);
}

export interface RunnerProgress {
  runId: string;
  iteration: number;
  requestName: string;
  status: 'pass' | 'fail' | 'skip';
  done: number;
  total: number;
}

export function onRunnerProgress(handler: (p: RunnerProgress) => void): () => void {
  return EventsOn('runner:progress', handler);
}

export interface MockLogEntry {
  collectionId: string;
  method: string;
  path: string;
  matched?: string;
  status: number;
  ts: number;
}

export function onMockLog(handler: (p: MockLogEntry) => void): () => void {
  return EventsOn('mock:log', handler);
}

export function onProtoMessage(handler: (m: InboundMsg) => void): () => void {
  return EventsOn('proto:message', handler);
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
