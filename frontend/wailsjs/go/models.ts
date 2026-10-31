export namespace dto {
	
	export class AICompleteRequest {
	    planId: string;
	    taskId: string;
	    done: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AICompleteRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.planId = source["planId"];
	        this.taskId = source["taskId"];
	        this.done = source["done"];
	    }
	}
	export class AIStudyTask {
	    id: string;
	    assignmentId: number;
	    title: string;
	    startAt: string;
	    minutes: number;
	    steps: string[];
	    reason: string;
	    done: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AIStudyTask(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.assignmentId = source["assignmentId"];
	        this.title = source["title"];
	        this.startAt = source["startAt"];
	        this.minutes = source["minutes"];
	        this.steps = source["steps"];
	        this.reason = source["reason"];
	        this.done = source["done"];
	    }
	}
	export class AIConfirmRequest {
	    jobId: string;
	    tasks: AIStudyTask[];
	
	    static createFrom(source: any = {}) {
	        return new AIConfirmRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.jobId = source["jobId"];
	        this.tasks = this.convertValues(source["tasks"], AIStudyTask);
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
	export class AITimeRange {
	    startAt: string;
	    endAt: string;
	
	    static createFrom(source: any = {}) {
	        return new AITimeRange(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.startAt = source["startAt"];
	        this.endAt = source["endAt"];
	    }
	}
	export class AIInput {
	    kind: string;
	    question: string;
	    assignmentIds: number[];
	    startAt: string;
	    endAt: string;
	    snapshot: string;
	    blocked: AITimeRange[];
	
	    static createFrom(source: any = {}) {
	        return new AIInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.question = source["question"];
	        this.assignmentIds = source["assignmentIds"];
	        this.startAt = source["startAt"];
	        this.endAt = source["endAt"];
	        this.snapshot = source["snapshot"];
	        this.blocked = this.convertValues(source["blocked"], AITimeRange);
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
	export class AIResult {
	    ok: boolean;
	    data?: Record<string, any>;
	    error?: APIError;
	
	    static createFrom(source: any = {}) {
	        return new AIResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.data = source["data"];
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
	export class AISettingsRequest {
	    enabled: boolean;
	    baseUrl: string;
	    model: string;
	    maxInputChars: number;
	    maxOutputTokens: number;
	    monthlyTokenLimit: number;
	    timeoutSeconds: number;
	    apiKey: string;
	    clearKey: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AISettingsRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.baseUrl = source["baseUrl"];
	        this.model = source["model"];
	        this.maxInputChars = source["maxInputChars"];
	        this.maxOutputTokens = source["maxOutputTokens"];
	        this.monthlyTokenLimit = source["monthlyTokenLimit"];
	        this.timeoutSeconds = source["timeoutSeconds"];
	        this.apiKey = source["apiKey"];
	        this.clearKey = source["clearKey"];
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
	export class SessionImportRequest {
	    session: string;
	
	    static createFrom(source: any = {}) {
	        return new SessionImportRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.session = source["session"];
	    }
	}
	export class SessionImportResult {
	    ok: boolean;
	    loginState?: string;
	    displayName?: string;
	    error?: APIError;
	
	    static createFrom(source: any = {}) {
	        return new SessionImportResult(source);
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
	export class SettingsView {
	    logLevel: string;
	    rawEnabled: boolean;
	    rawRetentionDays: number;
	    includeHistory: boolean;
	    theme: string;
	    dataDir: string;
	    version: string;
	    enabledPlatforms: string[];
	    changelogSeenVersion: string;
	    firstRun: boolean;
	
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
	        this.enabledPlatforms = source["enabledPlatforms"];
	        this.changelogSeenVersion = source["changelogSeenVersion"];
	        this.firstRun = source["firstRun"];
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
	
	export class UpdateCheckResult {
	    ok: boolean;
	    current: string;
	    latest?: string;
	    releaseName?: string;
	    releaseUrl?: string;
	    hasUpdate: boolean;
	    checkedAt?: string;
	    skipped?: boolean;
	    error?: APIError;
	
	    static createFrom(source: any = {}) {
	        return new UpdateCheckResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.current = source["current"];
	        this.latest = source["latest"];
	        this.releaseName = source["releaseName"];
	        this.releaseUrl = source["releaseUrl"];
	        this.hasUpdate = source["hasUpdate"];
	        this.checkedAt = source["checkedAt"];
	        this.skipped = source["skipped"];
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

}

