// @vitest-environment jsdom

import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

const {
    docMock,
    getDocMock,
    serverTimestampMock,
    setDocMock,
    updateDocMock,
} = vi.hoisted(() => ({
    docMock: vi.fn((...segments) => segments.join("/")),
    getDocMock: vi.fn(),
    serverTimestampMock: vi.fn(() => "server-timestamp"),
    setDocMock: vi.fn(),
    updateDocMock: vi.fn(),
}));

vi.mock("../firebase", () => ({ db: "db" }));

vi.mock("firebase/firestore", () => ({
    arrayRemove: vi.fn(),
    arrayUnion: vi.fn(),
    doc: docMock,
    getDoc: getDocMock,
    serverTimestamp: serverTimestampMock,
    setDoc: setDocMock,
    updateDoc: updateDocMock,
}));

let touchCurrentWebPushEndpoint;

describe("web push VAPID rotation", () => {
    beforeAll(async () => {
        vi.stubEnv("VITE_ENABLE_WEB_PUSH", "true");
        vi.stubEnv("VITE_WEB_PUSH_PUBLIC_KEY", "BAUG");
        ({ touchCurrentWebPushEndpoint } = await import("./webPush"));
    });

    beforeEach(() => {
        docMock.mockClear();
        getDocMock.mockReset();
        serverTimestampMock.mockClear();
        setDocMock.mockReset();
        updateDocMock.mockReset();

        Object.defineProperty(window, "PushManager", {
            configurable: true,
            value: function PushManager() { },
        });
        Object.defineProperty(window, "Notification", {
            configurable: true,
            value: { permission: "granted" },
        });
    });

    it("replaces a subscription bound to the previous VAPID key", async () => {
        const unsubscribe = vi.fn().mockResolvedValue(true);
        const oldSubscription = {
            endpoint: "https://push.example.test/old",
            options: { applicationServerKey: new Uint8Array([1, 2, 3]).buffer },
            unsubscribe,
            toJSON: () => ({ keys: { p256dh: "old-p256dh", auth: "old-auth" } }),
        };
        const newSubscription = {
            endpoint: "https://push.example.test/new",
            options: { applicationServerKey: new Uint8Array([4, 5, 6]).buffer },
            toJSON: () => ({ keys: { p256dh: "new-p256dh", auth: "new-auth" } }),
        };
        const subscribe = vi.fn().mockResolvedValue(newSubscription);
        const registration = {
            pushManager: {
                getSubscription: vi.fn().mockResolvedValue(oldSubscription),
                subscribe,
            },
        };
        Object.defineProperty(navigator, "serviceWorker", {
            configurable: true,
            value: { register: vi.fn().mockResolvedValue(registration) },
        });
        getDocMock.mockResolvedValue({ exists: () => false });

        await expect(touchCurrentWebPushEndpoint("user-1")).resolves.toBe(true);

        expect(unsubscribe).toHaveBeenCalledTimes(1);
        expect(subscribe).toHaveBeenCalledWith({
            userVisibleOnly: true,
            applicationServerKey: new Uint8Array([4, 5, 6]),
        });
        expect(setDocMock).toHaveBeenCalledTimes(2);
        expect(setDocMock.mock.calls[0][1]).toMatchObject({ active: false });
        expect(setDocMock.mock.calls[1][1]).toMatchObject({
            endpoint: newSubscription.endpoint,
            p256dh: "new-p256dh",
            auth: "new-auth",
            active: true,
        });
        expect(setDocMock.mock.calls.some(([, value]) => "webPushEnabled" in value)).toBe(false);
    });
});
