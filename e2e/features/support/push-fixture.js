import { createECDH, randomBytes, randomUUID } from "node:crypto";

export function createPushIdentity() {
    const key = createECDH("prime256v1");
    return {
        endpoint: `https://push.example.test/journey/${randomUUID()}`,
        keys: { p256dh: key.generateKeys().toString("base64url"), auth: randomBytes(16).toString("base64url") },
    };
}

export function pushBoundaryInit({ identity, response, origin }) {
    if (location.origin !== origin) return;
    if (!("Notification" in window) || !("PushManager" in window) || !("serviceWorker" in navigator)) {
        throw new Error("Push fixture requires native browser push capabilities");
    }
    const storageKey = "journey-push-boundary";
    const state = JSON.parse(localStorage.getItem(storageKey) ?? "null") ?? {
        permission: "default", requestCalls: 0, subscribeCalls: 0, unsubscribeCalls: 0,
        enableClicks: 0, unsolicitedRequests: 0,
        subscribed: false, applicationServerKey: [], registrations: [],
    };
    const persist = () => localStorage.setItem(storageKey, JSON.stringify(state));
    document.addEventListener("click", (event) => {
        const button = event.target.closest?.("button");
        if (event.isTrusted && button?.textContent.trim() === "Enable Push") {
            state.enableClicks += 1;
            persist();
        }
    }, true);
    const subscription = () => ({
        endpoint: identity.endpoint,
        expirationTime: null,
        options: { userVisibleOnly: true, applicationServerKey: new Uint8Array(state.applicationServerKey).buffer },
        toJSON: () => ({ endpoint: identity.endpoint, expirationTime: null, keys: { ...identity.keys } }),
        unsubscribe: async () => {
            state.unsubscribeCalls += 1;
            state.subscribed = false;
            persist();
            return true;
        },
    });
    Object.defineProperty(Notification, "permission", { configurable: true, get: () => state.permission });
    Object.defineProperty(Notification, "requestPermission", {
        configurable: true, value: async () => {
            state.requestCalls += 1;
            if (state.requestCalls > state.enableClicks) state.unsolicitedRequests += 1;
            state.permission = response;
            persist();
            return response;
        }
    });
    Object.defineProperty(PushManager.prototype, "getSubscription", { configurable: true, value: async () => state.subscribed ? subscription() : null });
    Object.defineProperty(PushManager.prototype, "subscribe", {
        configurable: true, value: async (options) => {
            if (state.permission !== "granted") throw new Error("Synthetic subscription requires voluntary granted permission");
            const key = new Uint8Array(options.applicationServerKey);
            if (!options.userVisibleOnly || key.length !== 65 || key[0] !== 4) throw new Error("Invalid application subscription options");
            state.subscribeCalls += 1;
            state.subscribed = true;
            state.applicationServerKey = Array.from(key);
            persist();
            return subscription();
        }
    });
    const register = navigator.serviceWorker.register.bind(navigator.serviceWorker);
    navigator.serviceWorker.register = async (url, options) => {
        const registration = await register(url, options);
        state.registrations.push(new URL(url, location.href).pathname);
        persist();
        return registration;
    };
    window.__journeyPush = { snapshot: () => ({ ...JSON.parse(JSON.stringify(state)), endpoint: identity.endpoint }) };
}

export async function installPushBoundary(context, response) {
    if (!["granted", "denied"].includes(response)) throw new Error("Unsupported fixture permission response");
    const identity = createPushIdentity();
    const origin = new URL(process.env.E2E_BASE_URL ?? "http://127.0.0.1:3000").origin;
    await context.addInitScript(pushBoundaryInit, { identity, response, origin });
    const deliveryRequests = [];
    await context.route("https://push.example.test/**", (route) => {
        deliveryRequests.push(route.request().method());
        return route.abort("blockedbyclient");
    });
    return { ...identity, deliveryRequests };
}
