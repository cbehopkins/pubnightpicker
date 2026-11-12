export class LastOrdersApiError extends Error {
    constructor(code, message) {
        super(message);
        this.name = "LastOrdersApiError";
        this.code = code;
    }
}

export function lastOrdersApiURL(path, query = null) {
    const configured = import.meta.env.VITE_LAST_ORDERS_API_BASE_URL || "http://localhost:8081";
    try {
        const base = new URL(configured);
        const loopback = ["localhost", "127.0.0.1", "[::1]"].includes(base.hostname);
        if ((base.protocol !== "https:" && !(base.protocol === "http:" && loopback)) ||
            base.username || base.password || base.search || base.hash) {
            throw new Error("Invalid API base URL");
        }
        base.pathname = `${base.pathname.replace(/\/+$/, "")}/`;
        const target = new URL(path, base);
        if (target.origin !== base.origin || !target.pathname.startsWith(base.pathname) || target.search || target.hash) {
            throw new Error("Invalid API path");
        }
        if (query !== null) {
            target.search = new URLSearchParams(query).toString();
        }
        return target.toString();
    } catch {
        throw new LastOrdersApiError("configuration_error", "The Last Orders API URL is not configured correctly.");
    }
}