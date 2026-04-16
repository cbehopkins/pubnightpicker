import { db } from "../firebase";
import {
    doc,
    getDoc,
    serverTimestamp,
    setDoc,
    updateDoc,
} from "firebase/firestore";

const WEB_PUSH_SW_PATH = "/push-sw.js";
function parseBooleanEnv(value) {
    if (value === undefined || value === null) {
        return false;
    }
    const normalized = String(value)
        .trim()
        .replace(/^['\"]|['\"]$/g, "")
        .toLowerCase();
    return normalized === "true";
}

const FEATURE_ENABLED = parseBooleanEnv(import.meta.env.VITE_ENABLE_WEB_PUSH);
const PUBLIC_VAPID_KEY = import.meta.env.VITE_WEB_PUSH_PUBLIC_KEY || "";

function supportsWebPush() {
    return (
        typeof window !== "undefined" &&
        "serviceWorker" in navigator &&
        "PushManager" in window &&
        "Notification" in window
    );
}

function hashEndpoint(endpoint) {
    let hash = 2166136261;
    for (let index = 0; index < endpoint.length; index += 1) {
        hash ^= endpoint.charCodeAt(index);
        hash +=
            (hash << 1) + (hash << 4) + (hash << 7) + (hash << 8) + (hash << 24);
    }
    return `ep_${(hash >>> 0).toString(16)}`;
}

function decodeBase64Url(value) {
    const padding = "=".repeat((4 - (value.length % 4)) % 4);
    const normalized = (value + padding).replace(/-/g, "+").replace(/_/g, "/");
    const raw = window.atob(normalized);
    const output = new Uint8Array(raw.length);
    for (let index = 0; index < raw.length; index += 1) {
        output[index] = raw.charCodeAt(index);
    }
    return output;
}

function subscriptionKeys(subscription) {
    const json = subscription.toJSON();
    return {
        endpoint: subscription.endpoint,
        p256dh: json.keys?.p256dh || null,
        auth: json.keys?.auth || null,
    };
}

function endpointRef(uid, endpointId) {
    return doc(db, "users", uid, "push_endpoints", endpointId);
}

async function ensureRegistration() {
    return navigator.serviceWorker.register(WEB_PUSH_SW_PATH);
}

async function getCurrentSubscription() {
    const registration = await ensureRegistration();
    return registration.pushManager.getSubscription();
}

async function subscribeCurrentBrowser() {
    if (!PUBLIC_VAPID_KEY) {
        throw new Error("Missing VITE_WEB_PUSH_PUBLIC_KEY");
    }
    const registration = await ensureRegistration();
    const existing = await registration.pushManager.getSubscription();
    if (existing) {
        return existing;
    }
    return registration.pushManager.subscribe({
        userVisibleOnly: true,
        applicationServerKey: decodeBase64Url(PUBLIC_VAPID_KEY),
    });
}

async function upsertEndpointDoc(uid, subscription) {
    const { endpoint, p256dh, auth } = subscriptionKeys(subscription);
    const endpointId = hashEndpoint(endpoint);
    const endpointDoc = endpointRef(uid, endpointId);
    const current = await getDoc(endpointDoc);
    await setDoc(
        endpointDoc,
        {
            endpoint,
            p256dh,
            auth,
            active: true,
            createdAt: current.exists() ? current.data()?.createdAt || serverTimestamp() : serverTimestamp(),
            lastSeenAt: serverTimestamp(),
            disabledAt: null,
            userAgent: navigator.userAgent || null,
            platform: navigator.platform || null,
            appVersion: import.meta.env.VITE_APP_VERSION || null,
        },
        { merge: true },
    );
    return endpointId;
}

export function webPushStatus() {
    return {
        featureEnabled: FEATURE_ENABLED,
        supported: supportsWebPush(),
        permission:
            typeof Notification === "undefined" ? "unsupported" : Notification.permission,
    };
}

export async function setWebPushPreference(uid, enabled) {
    await setDoc(
        doc(db, "users", uid),
        {
            webPushEnabled: enabled,
        },
        { merge: true },
    );
}

export async function enableWebPush(uid) {
    if (!FEATURE_ENABLED) {
        throw new Error("Web push is disabled by feature flag");
    }
    if (!supportsWebPush()) {
        throw new Error("This browser does not support web push");
    }
    const permission = await Notification.requestPermission();
    if (permission !== "granted") {
        throw new Error("Notification permission denied");
    }
    const subscription = await subscribeCurrentBrowser();
    const endpointId = await upsertEndpointDoc(uid, subscription);
    await setWebPushPreference(uid, true);
    return { endpointId };
}

export async function deactivateCurrentWebPushEndpoint(uid, { unsubscribe = false } = {}) {
    if (!supportsWebPush()) {
        return { endpointId: null };
    }
    const subscription = await getCurrentSubscription();
    if (!subscription) {
        await setWebPushPreference(uid, false);
        return { endpointId: null };
    }
    const { endpoint } = subscriptionKeys(subscription);
    const endpointId = hashEndpoint(endpoint);
    await setDoc(
        endpointRef(uid, endpointId),
        {
            active: false,
            disabledAt: serverTimestamp(),
            lastSeenAt: serverTimestamp(),
        },
        { merge: true },
    );
    if (unsubscribe) {
        await subscription.unsubscribe();
    }
    await setWebPushPreference(uid, false);
    return { endpointId };
}

export async function touchCurrentWebPushEndpoint(uid) {
    if (!FEATURE_ENABLED || !supportsWebPush()) {
        return false;
    }
    const subscription = await getCurrentSubscription();
    if (!subscription) {
        return false;
    }
    const { endpoint } = subscriptionKeys(subscription);
    const endpointId = hashEndpoint(endpoint);
    try {
        await updateDoc(endpointRef(uid, endpointId), {
            lastSeenAt: serverTimestamp(),
            active: true,
            disabledAt: null,
        });
    } catch {
        await upsertEndpointDoc(uid, subscription);
    }
    return true;
}

export async function registerPushServiceWorker() {
    if (!FEATURE_ENABLED || !supportsWebPush()) {
        return null;
    }
    return ensureRegistration();
}
