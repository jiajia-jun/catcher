export namespace dto {
	
	export class APIError {
	    code: string;
	    message: string;
	    detail?: string;
	    rawPath?: string;
	
	    static createFrom(source: any = {}) {
	        return new APIError(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.message = source["message"];
	        this.detail = source["detail"];
	        this.rawPath = source["rawPath"];
	    }
	}
	export class APIResult {
	    ok: boolean;
	    error?: APIError;
	
	    static createFrom(source: any = {}) {
	        return new APIResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.error = this.convertValues(source["error"], APIError);
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
	export class AssignmentFilter {
	    platforms?: string[];
	    courseIds?: number[];
	    statuses?: string[];
	    showSubmitted: boolean;
	    keyword?: string;
	    termOnly: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AssignmentFilter(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.platforms = source["platforms"];
	        this.courseIds = source["courseIds"];
	        this.statuses = source["statuses"];
	        this.showSubmitted = source["showSubmitted"];
	        this.keyword = source["keyword"];
	        this.termOnly = source["termOnly"];
	    }
	}
	export class AssignmentView {
	    id: number;
	    platform: string;
	    courseName: string;
	    courseExternalId: string;
	    title: string;
	    status: string;
	    platformStatus: string;
	    manualStatus?: string;
	    manualStatusAt?: string;
	    phase: string;
	    deadlineAt?: string;
	    deadlineLocal?: string;
	    deadlineSource: string;
	    deadlineRaw?: string;
	    detailUrl?: string;
	    firstSeenAt: string;
	    lastSeenAt: string;
	
	    static createFrom(source: any = {}) {
	        return new AssignmentView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.platform = source["platform"];
	        this.courseName = source["courseName"];
	        this.courseExternalId = source["courseExternalId"];
	        this.title = source["title"];
	        this.status = source["status"];
	        this.platformStatus = source["platformStatus"];
	        this.manualStatus = source["manualStatus"];
	        this.manualStatusAt = source["manualStatusAt"];
	        this.phase = source["phase"];
	        this.deadlineAt = source["deadlineAt"];
	        this.deadlineLocal = source["deadlineLocal"];
	        this.deadlineSource = source["deadlineSource"];
	        this.deadlineRaw = source["deadlineRaw"];
	        this.detailUrl = source["detailUrl"];
	        this.firstSeenAt = source["firstSeenAt"];
	        this.lastSeenAt = source["lastSeenAt"];
	    }
	}
	export class AssignmentListResult {
	    ok: boolean;
	    error?: APIError;
	    items: AssignmentView[];
	    total: number;
	
	    static createFrom(source: any = {}) {
	        return new AssignmentListResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.error = this.convertValues(source["error"], APIError);
	        this.items = this.convertValues(source["items"], AssignmentView);
	        this.total = source["total"];
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
	export class AssignmentStatusRequest {
	    id: number;
	    status: string;
	
	    static createFrom(source: any = {}) {
	        return new AssignmentStatusRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.status = source["status"];
	    }
	}
	export class AssignmentStatusResult {
	    ok: boolean;
	    item?: AssignmentView;
	    error?: APIError;
	
	    static createFrom(source: any = {}) {
	        return new AssignmentStatusResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.item = this.convertValues(source["item"], AssignmentView);
	        this.error = this.convertValues(source["error"], APIError);
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
	
	export class CountsView {
	    openUnsubmitted: number;
	    overdueUnsubmitted: number;
	    unknown: number;
	    submitted: number;
	
	    static createFrom(source: any = {}) {
	        return new CountsView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.openUnsubmitted = source["openUnsubmitted"];
	        this.overdueUnsubmitted = source["overdueUnsubmitted"];
	        this.unknown = source["unknown"];
	        this.submitted = source["submitted"];
	    }
	}
	export class PlatformView {
	    platform: string;
	    displayName: string;
	    loginState: string;
	    lastRunAt?: string;
	    lastRunState?: string;
	    lastRunId?: number;
	    counts: CountsView;
	    error?: APIError;
	    experimental: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PlatformView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.platform = source["platform"];
	        this.displayName = source["displayName"];
	        this.loginState = source["loginState"];
	        this.lastRunAt = source["lastRunAt"];
	        this.lastRunState = source["lastRunState"];
	        this.lastRunId = source["lastRunId"];
	        this.counts = this.convertValues(source["counts"], CountsView);
	        this.error = this.convertValues(source["error"], APIError);
	        this.experimental = source["experimental"];
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
	export class BootstrapData {
	    platforms: PlatformView[];
	    stats: CountsView;
	    dataDir: string;
	    version: string;
	
	    static createFrom(source: any = {}) {
	        return new BootstrapData(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.platforms = this.convertValues(source["platforms"], PlatformView);
	        this.stats = this.convertValues(source["stats"], CountsView);
	        this.dataDir = source["dataDir"];
	        this.version = source["version"];
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
	export class BootstrapResult {
	    ok: boolean;
	    error?: APIError;
	    data?: BootstrapData;
	
	    static createFrom(source: any = {}) {
	        return new BootstrapResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.error = this.convertValues(source["error"], APIError);
	        this.data = this.convertValues(source["data"], BootstrapData);
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
	
	export class FetchIssueView {
	    scope: string;
	    ref?: string;
	    kind: string;
	    message: string;
	    rawPath?: string;
	
	    static createFrom(source: any = {}) {
	        return new FetchIssueView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.scope = source["scope"];
	        this.ref = source["ref"];
	        this.kind = source["kind"];
	        this.message = source["message"];
	        this.rawPath = source["rawPath"];
	    }
	}
	export class FetchIssuesResult {
	    ok: boolean;
	    items: FetchIssueView[];
	    error?: APIError;
	
	    static createFrom(source: any = {}) {
	        return new FetchIssuesResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.items = this.convertValues(source["items"], FetchIssueView);
	        this.error = this.convertValues(source["error"], APIError);
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
	export class PasswordLoginRequest {
	    phone: string;
	    password: string;
	    remember: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PasswordLoginRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.phone = source["phone"];
	        this.password = source["password"];
	        this.remember = source["remember"];
	    }
	}
	export class PasswordLoginResult {
	    ok: boolean;
	    loginState?: string;
	    displayName?: string;
	    error?: APIError;
	
	    static createFrom(source: any = {}) {
	        return new PasswordLoginResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.loginState = source["loginState"];
	        this.displayName = source["displayName"];
	        this.error = this.convertValues(source["error"], APIError);
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
	
	export class RefreshStartResult {
	    ok: boolean;
	    jobId?: string;
	    platforms?: string[];
	    alreadyRunning: boolean;
	    error?: APIError;
	
	    static createFrom(source: any = {}) {
	        return new RefreshStartResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.jobId = source["jobId"];
	        this.platforms = source["platforms"];
	        this.alreadyRunning = source["alreadyRunning"];
	        this.error = this.convertValues(source["error"], APIError);
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
	export class SettingsView {
	    logLevel: string;
	    rawEnabled: boolean;
	    rawRetentionDays: number;
	    includeHistory: boolean;
	    theme: string;
	    dataDir: string;
	    version: string;
	
	    static createFrom(source: any = {}) {
	        return new SettingsView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.logLevel = source["logLevel"];
	        this.rawEnabled = source["rawEnabled"];
	        this.rawRetentionDays = source["rawRetentionDays"];
	        this.includeHistory = source["includeHistory"];
	        this.theme = source["theme"];
	        this.dataDir = source["dataDir"];
	        this.version = source["version"];
	    }
	}
	export class SettingsResult {
	    ok: boolean;
	    error?: APIError;
	    settings?: SettingsView;
	
	    static createFrom(source: any = {}) {
	        return new SettingsResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.error = this.convertValues(source["error"], APIError);
	        this.settings = this.convertValues(source["settings"], SettingsView);
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

