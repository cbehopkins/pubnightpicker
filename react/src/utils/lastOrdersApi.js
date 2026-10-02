import { auth } from "../firebase";

export class LastOrdersApiError extends Error {
    constructor(code, message) {
        super(message);
        this.name = "LastOrdersApiError";
        this.code = code;
    }
}

export function lastOrdersApiURL(path) {
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
        return target.toString();
    } catch {
        throw new LastOrdersApiError("configuration_error", "The Last Orders API URL is not configured correctly.");
    }
}

export async function requestLastOrders(path, { body = null, signal = new AbortController().signal } = {}) {
    const url = lastOrdersApiURL(path);
    const user = auth.currentUser;
    if (!user) {
        throw new LastOrdersApiError("signed_out", "Please sign in before contacting Last Orders.");
    }
    signal.throwIfAborted();
    const ensureCurrentSession = () => {
        signal.throwIfAborted();
        if (auth.currentUser?.uid !== user.uid) {
            throw new LastOrdersApiError("signed_out", "Your sign-in session changed. Please try again.");
        }
    };
    return new Promise((resolve, reject) => {
        const abort = () => reject(signal.reason);
        signal.addEventListener("abort", abort, { once: true });
        const execute = async () => {
            let token;
            try {
                token = await user.getIdToken();
            } catch {
                ensureCurrentSession();
                throw new LastOrdersApiError("token_error", "Unable to obtain a sign-in token. Please sign in again.");
            }
            ensureCurrentSession();
            let response;
            try {
                response = await fetch(url, {
                    method: "POST",
                    headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" },
                    body: JSON.stringify(body),
                    credentials: "omit",
                    cache: "no-store",
                    redirect: "error",
                    signal,
                });
            } catch {
                ensureCurrentSession();
                throw new LastOrdersApiError("connection_error", "Unable to connect to Last Orders. Check the backend URL, connection and allowed origins.");
            }
            ensureCurrentSession();
            if (response.status === 401) {
                throw new LastOrdersApiError("unauthenticated", "Last Orders refused your sign-in token. Please sign in again.");
            }
            if (response.status === 403) {
                throw new LastOrdersApiError("forbidden", "Last Orders refused access to this request.");
            }
            if (!response.ok) {
                throw new LastOrdersApiError("backend_error", "Last Orders could not complete the request. Please try again.");
            }
            const contentType = response.headers.get("Content-Type")?.split(";")[0].trim().toLowerCase();
            if (contentType !== "application/json") {
                throw new LastOrdersApiError("invalid_response", "Last Orders returned an unexpected response.");
            }
            let data;
            try {
                data = await response.json();
            } catch {
                ensureCurrentSession();
                throw new LastOrdersApiError("invalid_response", "Last Orders returned an unexpected response.");
            }
            ensureCurrentSession();
            return data;
        };
        execute().then(resolve, reject).finally(() => signal.removeEventListener("abort", abort));
    });
}
