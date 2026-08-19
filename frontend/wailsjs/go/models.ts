export namespace auth {

	export class Token {
	    accessToken: string;
	    refreshToken?: string;
	    tokenType?: string;
	    expiresAt?: number;
	    scope?: string;

	    static createFrom(source: any = {}) {
	        return new Token(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.accessToken = source["accessToken"];
	        this.refreshToken = source["refreshToken"];
	        this.tokenType = source["tokenType"];
	        this.expiresAt = source["expiresAt"];
	        this.scope = source["scope"];
	    }
	}

}

export namespace binding {

	export class MockStatus {
	    collectionId: string;
	    addr: string;
	    routes: number;

	    static createFrom(source: any = {}) {
	        return new MockStatus(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.collectionId = source["collectionId"];
	        this.addr = source["addr"];
	        this.routes = source["routes"];
	    }
	}
	export class ProxySettings {
	    mode: string;
	    url?: string;
	    username?: string;
	    password?: string;
	    passwordSet?: boolean;
	    clearPassword?: boolean;

	    static createFrom(source: any = {}) {
	        return new ProxySettings(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.url = source["url"];
	        this.username = source["username"];
	        this.password = source["password"];
	        this.passwordSet = source["passwordSet"];
	        this.clearPassword = source["clearPassword"];
	    }
	}
	export class NetworkStatus {
	    proxyMode: string;
	    proxySource: string;
	    proxyWarning?: string;
	    tlsActive: boolean;
	    tlsWarning?: string;

	    static createFrom(source: any = {}) {
	        return new NetworkStatus(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.proxyMode = source["proxyMode"];
	        this.proxySource = source["proxySource"];
	        this.proxyWarning = source["proxyWarning"];
	        this.tlsActive = source["tlsActive"];
	        this.tlsWarning = source["tlsWarning"];
	    }
	}

}
export namespace codegen {
	
	export class Target {
	    id: string;
	    name: string;
	
	    static createFrom(source: any = {}) {
	        return new Target(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	    }
	}

}

export namespace convert {
	
	export class ImportResult {
	    collection: model.Node;
	    children: model.Node[];
	    warnings: string[];
	
	    static createFrom(source: any = {}) {
	        return new ImportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.collection = this.convertValues(source["collection"], model.Node);
	        this.children = this.convertValues(source["children"], model.Node);
	        this.warnings = source["warnings"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace graphql {
	
	export class FieldSummary {
	    name: string;
	    description?: string;
	    args?: string;
	    returnType: string;
	    returnKind: string;
	
	    static createFrom(source: any = {}) {
	        return new FieldSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.args = source["args"];
	        this.returnType = source["returnType"];
	        this.returnKind = source["returnKind"];
	    }
	}
	export class IntrospectConfig {
	    url: string;
	    headers: Record<string, string>;
	    timeoutMs?: number;
	
	    static createFrom(source: any = {}) {
	        return new IntrospectConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.headers = source["headers"];
	        this.timeoutMs = source["timeoutMs"];
	    }
	}
	export class Result {
	    schemaJson: string;
	    queries: FieldSummary[];
	    mutations: FieldSummary[];
	    subscriptions?: FieldSummary[];
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.schemaJson = source["schemaJson"];
	        this.queries = this.convertValues(source["queries"], FieldSummary);
	        this.mutations = this.convertValues(source["mutations"], FieldSummary);
	        this.subscriptions = this.convertValues(source["subscriptions"], FieldSummary);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace grpcclient {
	
	export class CallResult {
	    response: string;
	    durationMs: number;
	    headers?: Record<string, string>;
	    trailers?: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new CallResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.response = source["response"];
	        this.durationMs = source["durationMs"];
	        this.headers = source["headers"];
	        this.trailers = source["trailers"];
	    }
	}
	export class ConnectConfig {
	    target: string;
	    useTls: boolean;
	    insecureTls?: boolean;
	    timeoutMs?: number;
	
	    static createFrom(source: any = {}) {
	        return new ConnectConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.target = source["target"];
	        this.useTls = source["useTls"];
	        this.insecureTls = source["insecureTls"];
	        this.timeoutMs = source["timeoutMs"];
	    }
	}
	export class MethodInfo {
	    service: string;
	    method: string;
	    fullName: string;
	    clientStream: boolean;
	    serverStream: boolean;
	    inputExample: string;
	
	    static createFrom(source: any = {}) {
	        return new MethodInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.service = source["service"];
	        this.method = source["method"];
	        this.fullName = source["fullName"];
	        this.clientStream = source["clientStream"];
	        this.serverStream = source["serverStream"];
	        this.inputExample = source["inputExample"];
	    }
	}

}

export namespace httpengine {
	
	export class TLSSettings {
	    caCertPath?: string;
	    clientCertPath?: string;
	    clientKeyPath?: string;
	
	    static createFrom(source: any = {}) {
	        return new TLSSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.caCertPath = source["caCertPath"];
	        this.clientCertPath = source["clientCertPath"];
	        this.clientKeyPath = source["clientKeyPath"];
	    }
	}

}

export namespace mock {
	
	export class Options {
	    port?: number;
	    delayMs?: number;
	
	    static createFrom(source: any = {}) {
	        return new Options(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.port = source["port"];
	        this.delayMs = source["delayMs"];
	    }
	}

}

export namespace model {
	
	export class Auth {
	    type: string;
	    params?: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new Auth(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.params = source["params"];
	    }
	}
	export class FormItem {
	    key: string;
	    type: string;
	    value?: string;
	    path?: string;
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new FormItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.type = source["type"];
	        this.value = source["value"];
	        this.path = source["path"];
	        this.enabled = source["enabled"];
	    }
	}
	export class Body {
	    kind: string;
	    language?: string;
	    text?: string;
	    items?: FormItem[];
	    path?: string;
	    query?: string;
	    variables?: string;
	
	    static createFrom(source: any = {}) {
	        return new Body(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.language = source["language"];
	        this.text = source["text"];
	        this.items = this.convertValues(source["items"], FormItem);
	        this.path = source["path"];
	        this.query = source["query"];
	        this.variables = source["variables"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Cookie {
	    name: string;
	    value: string;
	    domain?: string;
	    path?: string;
	    expires?: number;
	    maxAge?: number;
	    httpOnly: boolean;
	    secure: boolean;
	    sameSite?: string;
	    hostOnly: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Cookie(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.value = source["value"];
	        this.domain = source["domain"];
	        this.path = source["path"];
	        this.expires = source["expires"];
	        this.maxAge = source["maxAge"];
	        this.httpOnly = source["httpOnly"];
	        this.secure = source["secure"];
	        this.sameSite = source["sameSite"];
	        this.hostOnly = source["hostOnly"];
	    }
	}
	export class Variable {
	    key: string;
	    value: string;
	    type: string;
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Variable(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.value = source["value"];
	        this.type = source["type"];
	        this.enabled = source["enabled"];
	    }
	}
	export class Environment {
	    id: string;
	    workspaceId: string;
	    name: string;
	    variables: Variable[];
	    isActive: boolean;
	    createdAt: number;
	    updatedAt: number;
	
	    static createFrom(source: any = {}) {
	        return new Environment(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.workspaceId = source["workspaceId"];
	        this.name = source["name"];
	        this.variables = this.convertValues(source["variables"], Variable);
	        this.isActive = source["isActive"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RequestSettings {
	    timeoutMs?: number;
	    followRedirects: boolean;
	    maxRedirects?: number;
	    verifyTls: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RequestSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.timeoutMs = source["timeoutMs"];
	        this.followRedirects = source["followRedirects"];
	        this.maxRedirects = source["maxRedirects"];
	        this.verifyTls = source["verifyTls"];
	    }
	}
	export class KV {
	    key: string;
	    value: string;
	    enabled: boolean;
	    description?: string;
	
	    static createFrom(source: any = {}) {
	        return new KV(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.value = source["value"];
	        this.enabled = source["enabled"];
	        this.description = source["description"];
	    }
	}
	export class HttpRequest {
	    method: string;
	    url: string;
	    params: KV[];
	    headers: KV[];
	    body: Body;
	    auth: Auth;
	    settings: RequestSettings;
	    preScript?: string;
	    testScript?: string;
	
	    static createFrom(source: any = {}) {
	        return new HttpRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.method = source["method"];
	        this.url = source["url"];
	        this.params = this.convertValues(source["params"], KV);
	        this.headers = this.convertValues(source["headers"], KV);
	        this.body = this.convertValues(source["body"], Body);
	        this.auth = this.convertValues(source["auth"], Auth);
	        this.settings = this.convertValues(source["settings"], RequestSettings);
	        this.preScript = source["preScript"];
	        this.testScript = source["testScript"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Example {
	    id: string;
	    nodeId: string;
	    name: string;
	    requestSnap?: HttpRequest;
	    status: number;
	    headers: KV[];
	    body?: string;
	    createdAt: number;
	    updatedAt: number;
	
	    static createFrom(source: any = {}) {
	        return new Example(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.nodeId = source["nodeId"];
	        this.name = source["name"];
	        this.requestSnap = this.convertValues(source["requestSnap"], HttpRequest);
	        this.status = source["status"];
	        this.headers = this.convertValues(source["headers"], KV);
	        this.body = source["body"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class TestResult {
	    name: string;
	    pass: boolean;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new TestResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.pass = source["pass"];
	        this.error = source["error"];
	    }
	}
	export class Timing {
	    dnsMs: number;
	    connectMs: number;
	    tlsMs: number;
	    ttfbMs: number;
	    downloadMs: number;
	    totalMs: number;
	
	    static createFrom(source: any = {}) {
	        return new Timing(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.dnsMs = source["dnsMs"];
	        this.connectMs = source["connectMs"];
	        this.tlsMs = source["tlsMs"];
	        this.ttfbMs = source["ttfbMs"];
	        this.downloadMs = source["downloadMs"];
	        this.totalMs = source["totalMs"];
	    }
	}
	export class HistoryDetail {
	    id: string;
	    workspaceId: string;
	    requestSnap: HttpRequest;
	    status: number;
	    durationMs: number;
	    sizeBytes: number;
	    timing: Timing;
	    respHeaders: KV[];
	    bodyRef?: string;
	    bodyInline?: string;
	    testResults?: TestResult[];
	    createdAt: number;
	
	    static createFrom(source: any = {}) {
	        return new HistoryDetail(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.workspaceId = source["workspaceId"];
	        this.requestSnap = this.convertValues(source["requestSnap"], HttpRequest);
	        this.status = source["status"];
	        this.durationMs = source["durationMs"];
	        this.sizeBytes = source["sizeBytes"];
	        this.timing = this.convertValues(source["timing"], Timing);
	        this.respHeaders = this.convertValues(source["respHeaders"], KV);
	        this.bodyRef = source["bodyRef"];
	        this.bodyInline = source["bodyInline"];
	        this.testResults = this.convertValues(source["testResults"], TestResult);
	        this.createdAt = source["createdAt"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class HistorySummary {
	    id: string;
	    workspaceId: string;
	    method: string;
	    url: string;
	    status: number;
	    durationMs: number;
	    sizeBytes: number;
	    hasBody: boolean;
	    createdAt: number;
	
	    static createFrom(source: any = {}) {
	        return new HistorySummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.workspaceId = source["workspaceId"];
	        this.method = source["method"];
	        this.url = source["url"];
	        this.status = source["status"];
	        this.durationMs = source["durationMs"];
	        this.sizeBytes = source["sizeBytes"];
	        this.hasBody = source["hasBody"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class HistoryPage {
	    items: HistorySummary[];
	    nextCursor?: string;
	    hasMore: boolean;
	
	    static createFrom(source: any = {}) {
	        return new HistoryPage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.items = this.convertValues(source["items"], HistorySummary);
	        this.nextCursor = source["nextCursor"];
	        this.hasMore = source["hasMore"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class HistoryQuery {
	    search?: string;
	    limit?: number;
	    cursor?: string;
	    offset?: number;
	
	    static createFrom(source: any = {}) {
	        return new HistoryQuery(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.search = source["search"];
	        this.limit = source["limit"];
	        this.cursor = source["cursor"];
	        this.offset = source["offset"];
	    }
	}
	
	
	
	export class Node {
	    id: string;
	    workspaceId: string;
	    parentId?: string;
	    kind: string;
	    name: string;
	    sortOrder: number;
	    request?: HttpRequest;
	    auth?: Auth;
	    variables?: Variable[];
	    preScript?: string;
	    testScript?: string;
	    createdAt: number;
	    updatedAt: number;
	
	    static createFrom(source: any = {}) {
	        return new Node(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.workspaceId = source["workspaceId"];
	        this.parentId = source["parentId"];
	        this.kind = source["kind"];
	        this.name = source["name"];
	        this.sortOrder = source["sortOrder"];
	        this.request = this.convertValues(source["request"], HttpRequest);
	        this.auth = this.convertValues(source["auth"], Auth);
	        this.variables = this.convertValues(source["variables"], Variable);
	        this.preScript = source["preScript"];
	        this.testScript = source["testScript"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class NodeMove {
	    id: string;
	    parentId?: string;
	    sortOrder: number;

	    static createFrom(source: any = {}) {
	        return new NodeMove(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.parentId = source["parentId"];
	        this.sortOrder = source["sortOrder"];
	    }
	}
	export class NodeSummary {
	    id: string;
	    workspaceId: string;
	    parentId?: string;
	    kind: string;
	    name: string;
	    sortOrder: number;
	    method?: string;
	    createdAt: number;
	    updatedAt: number;

	    static createFrom(source: any = {}) {
	        return new NodeSummary(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.workspaceId = source["workspaceId"];
	        this.parentId = source["parentId"];
	        this.kind = source["kind"];
	        this.name = source["name"];
	        this.sortOrder = source["sortOrder"];
	        this.method = source["method"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	    }
	}
	
	export class ResponseBlobChunk {
	    offset: number;
	    bytesRead: number;
	    dataBase64: string;
	    eof: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ResponseBlobChunk(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.offset = source["offset"];
	        this.bytesRead = source["bytesRead"];
	        this.dataBase64 = source["dataBase64"];
	        this.eof = source["eof"];
	    }
	}
	export class ResponseBlobInfo {
	    ref: string;
	    sizeBytes: number;
	
	    static createFrom(source: any = {}) {
	        return new ResponseBlobInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ref = source["ref"];
	        this.sizeBytes = source["sizeBytes"];
	    }
	}
	export class ResponseBody {
	    inline: boolean;
	    text?: string;
	    blobRef?: string;
	    encoding?: string;
	
	    static createFrom(source: any = {}) {
	        return new ResponseBody(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.inline = source["inline"];
	        this.text = source["text"];
	        this.blobRef = source["blobRef"];
	        this.encoding = source["encoding"];
	    }
	}
	export class ResponseResult {
	    status: number;
	    statusText: string;
	    headers: KV[];
	    cookies: Cookie[];
	    body: ResponseBody;
	    timing: Timing;
	    sizeBytes: number;
	    testResults: TestResult[];
	    scriptLogs: string[];
	    historyId?: string;
	
	    static createFrom(source: any = {}) {
	        return new ResponseResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.statusText = source["statusText"];
	        this.headers = this.convertValues(source["headers"], KV);
	        this.cookies = this.convertValues(source["cookies"], Cookie);
	        this.body = this.convertValues(source["body"], ResponseBody);
	        this.timing = this.convertValues(source["timing"], Timing);
	        this.sizeBytes = source["sizeBytes"];
	        this.testResults = this.convertValues(source["testResults"], TestResult);
	        this.scriptLogs = source["scriptLogs"];
	        this.historyId = source["historyId"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SendContext {
	    requestId?: string;
	    workspaceId: string;
	    environmentId?: string;
	    variableOverrides?: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new SendContext(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.requestId = source["requestId"];
	        this.workspaceId = source["workspaceId"];
	        this.environmentId = source["environmentId"];
	        this.variableOverrides = source["variableOverrides"];
	    }
	}
	
	
	
	export class Workspace {
	    id: string;
	    name: string;
	    type: string;
	    createdAt: number;
	    updatedAt: number;
	
	    static createFrom(source: any = {}) {
	        return new Workspace(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.type = source["type"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	    }
	}

}

export namespace protocol {
	
	export class SessionConfig {
	    protocol: string;
	    url: string;
	    headers?: model.KV[];
	
	    static createFrom(source: any = {}) {
	        return new SessionConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.protocol = source["protocol"];
	        this.url = source["url"];
	        this.headers = this.convertValues(source["headers"], model.KV);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace runner {
	
	export class Options {
	    dataFile?: string;
	    dataFormat?: string;
	    stopOnError: boolean;
	    iterations?: number;
	
	    static createFrom(source: any = {}) {
	        return new Options(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.dataFile = source["dataFile"];
	        this.dataFormat = source["dataFormat"];
	        this.stopOnError = source["stopOnError"];
	        this.iterations = source["iterations"];
	    }
	}
	export class RequestResult {
	    iteration: number;
	    requestName: string;
	    nodeId: string;
	    status: number;
	    durationMs: number;
	    failed: boolean;
	    error?: string;
	    testResults: model.TestResult[];
	
	    static createFrom(source: any = {}) {
	        return new RequestResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.iteration = source["iteration"];
	        this.requestName = source["requestName"];
	        this.nodeId = source["nodeId"];
	        this.status = source["status"];
	        this.durationMs = source["durationMs"];
	        this.failed = source["failed"];
	        this.error = source["error"];
	        this.testResults = this.convertValues(source["testResults"], model.TestResult);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Report {
	    runId: string;
	    total: number;
	    passed: number;
	    failed: number;
	    skipped: number;
	    durationMs: number;
	    results: RequestResult[];
	    canceled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Report(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.runId = source["runId"];
	        this.total = source["total"];
	        this.passed = source["passed"];
	        this.failed = source["failed"];
	        this.skipped = source["skipped"];
	        this.durationMs = source["durationMs"];
	        this.results = this.convertValues(source["results"], RequestResult);
	        this.canceled = source["canceled"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace secrets {
	
	export class Status {
	    mode: string;
	    keyringAvailable: boolean;
	    fileExists: boolean;
	    fileUnlocked: boolean;
	    canStore: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.keyringAvailable = source["keyringAvailable"];
	        this.fileExists = source["fileExists"];
	        this.fileUnlocked = source["fileUnlocked"];
	        this.canStore = source["canStore"];
	    }
	}

}

export namespace sync {
	
	export class DavConfig {
	    url: string;
	    username: string;
	    password?: string;
	    passwordSet?: boolean;
	    clearPassword?: boolean;
	    omitSecrets: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DavConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.username = source["username"];
	        this.password = source["password"];
	        this.passwordSet = source["passwordSet"];
	        this.clearPassword = source["clearPassword"];
	        this.omitSecrets = source["omitSecrets"];
	    }
	}
	export class Report {
	    pushed: number;
	    pulled: number;
	    deleted: number;
	    remoteFresh: boolean;
	    syncedAt: number;
	    remote: string;
	
	    static createFrom(source: any = {}) {
	        return new Report(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.pushed = source["pushed"];
	        this.pulled = source["pulled"];
	        this.deleted = source["deleted"];
	        this.remoteFresh = source["remoteFresh"];
	        this.syncedAt = source["syncedAt"];
	        this.remote = source["remote"];
	    }
	}

}
