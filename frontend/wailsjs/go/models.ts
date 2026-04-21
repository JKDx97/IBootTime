export namespace agentproxy {
	
	export class RemoteClient {
	    client_id: string;
	    hostname: string;
	    ip: string;
	    os_version: string;
	    mac: string;
	    status: string;
	    registered_at: number;
	    last_seen: number;
	
	    static createFrom(source: any = {}) {
	        return new RemoteClient(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.client_id = source["client_id"];
	        this.hostname = source["hostname"];
	        this.ip = source["ip"];
	        this.os_version = source["os_version"];
	        this.mac = source["mac"];
	        this.status = source["status"];
	        this.registered_at = source["registered_at"];
	        this.last_seen = source["last_seen"];
	    }
	}
	export class RemoteTask {
	    task_id: string;
	    task_type: string;
	    params: any;
	    status: string;
	    result_output: string;
	    created_at: number;
	    completed_at: number;
	
	    static createFrom(source: any = {}) {
	        return new RemoteTask(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.task_id = source["task_id"];
	        this.task_type = source["task_type"];
	        this.params = source["params"];
	        this.status = source["status"];
	        this.result_output = source["result_output"];
	        this.created_at = source["created_at"];
	        this.completed_at = source["completed_at"];
	    }
	}
	export class TaskResponse {
	    task_id: string;
	    status: string;
	
	    static createFrom(source: any = {}) {
	        return new TaskResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.task_id = source["task_id"];
	        this.status = source["status"];
	    }
	}

}

export namespace isomgr {
	
	export class ISOInfo {
	    name: string;
	    path: string;
	    size: number;
	    sizeHR: string;
	    osType: string;
	    arch: string;
	    enabled: boolean;
	    unattendPath: string;
	
	    static createFrom(source: any = {}) {
	        return new ISOInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.size = source["size"];
	        this.sizeHR = source["sizeHR"];
	        this.osType = source["osType"];
	        this.arch = source["arch"];
	        this.enabled = source["enabled"];
	        this.unattendPath = source["unattendPath"];
	    }
	}

}

export namespace logger {
	
	export class LogEntry {
	    level: string;
	    message: string;
	    source: string;
	    // Go type: time
	    timestamp: any;
	
	    static createFrom(source: any = {}) {
	        return new LogEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.level = source["level"];
	        this.message = source["message"];
	        this.source = source["source"];
	        this.timestamp = this.convertValues(source["timestamp"], null);
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

export namespace netinfo {
	
	export class NetInterface {
	    name: string;
	    ip: string;
	    mac: string;
	    isUp: boolean;
	    isLoopback: boolean;
	
	    static createFrom(source: any = {}) {
	        return new NetInterface(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.ip = source["ip"];
	        this.mac = source["mac"];
	        this.isUp = source["isUp"];
	        this.isLoopback = source["isLoopback"];
	    }
	}

}

export namespace orchestrator {
	
	export class ServiceStatus {
	    dhcp: boolean;
	    tftp: boolean;
	    http: boolean;
	    running: boolean;
	    ip: string;
	    httpPort: number;
	    bootProtocol: string;
	    startupPhase: string;
	    startupProgress: number;
	    startupDetail: string;
	
	    static createFrom(source: any = {}) {
	        return new ServiceStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.dhcp = source["dhcp"];
	        this.tftp = source["tftp"];
	        this.http = source["http"];
	        this.running = source["running"];
	        this.ip = source["ip"];
	        this.httpPort = source["httpPort"];
	        this.bootProtocol = source["bootProtocol"];
	        this.startupPhase = source["startupPhase"];
	        this.startupProgress = source["startupProgress"];
	        this.startupDetail = source["startupDetail"];
	    }
	}

}

export namespace session {
	
	export class ClientSession {
	    mac: string;
	    ip: string;
	    arch: string;
	    state: string;
	    isoName: string;
	    bytesTransferred: number;
	    totalBytes: number;
	    progress: number;
	    speed: string;
	    // Go type: time
	    startedAt: any;
	    // Go type: time
	    lastSeen: any;
	    remoteAvailable: boolean;
	    remoteVncPort: number;
	    remotePassword: string;
	    assignedISO: string;
	
	    static createFrom(source: any = {}) {
	        return new ClientSession(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mac = source["mac"];
	        this.ip = source["ip"];
	        this.arch = source["arch"];
	        this.state = source["state"];
	        this.isoName = source["isoName"];
	        this.bytesTransferred = source["bytesTransferred"];
	        this.totalBytes = source["totalBytes"];
	        this.progress = source["progress"];
	        this.speed = source["speed"];
	        this.startedAt = this.convertValues(source["startedAt"], null);
	        this.lastSeen = this.convertValues(source["lastSeen"], null);
	        this.remoteAvailable = source["remoteAvailable"];
	        this.remoteVncPort = source["remoteVncPort"];
	        this.remotePassword = source["remotePassword"];
	        this.assignedISO = source["assignedISO"];
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

