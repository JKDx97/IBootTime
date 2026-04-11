export namespace isomgr {
	
	export class ISOInfo {
	    name: string;
	    path: string;
	    size: number;
	    sizeHR: string;
	    osType: string;
	    arch: string;
	    enabled: boolean;
	
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
	    bootProtocol: string;
	
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
	        this.bootProtocol = source["bootProtocol"];
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

