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
	    httpOnly: boolean;
	    secure: boolean;
	
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
	        this.httpOnly = source["httpOnly"];
	        this.secure = source["secure"];
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
	export class HistoryItem {
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
	        return new HistoryItem(source);
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
	export class HistoryQuery {
	    search?: string;
	    limit?: number;
	    offset?: number;
	
	    static createFrom(source: any = {}) {
	        return new HistoryQuery(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.search = source["search"];
	        this.limit = source["limit"];
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
	
	export class ResponseBody {
	    inline: boolean;
	    text?: string;
	    blobRef?: string;
	
	    static createFrom(source: any = {}) {
	        return new ResponseBody(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.inline = source["inline"];
	        this.text = source["text"];
	        this.blobRef = source["blobRef"];
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

