// @vitest-environment jsdom

import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import useWebPushLifecycle from "./useWebPushLifecycle";

const {
    deactivateCurrentWebPushEndpointMock,
    enableWebPushMock,
    getDocMock,
    hasCurrentWebPushSubscriptionMock,
    firestoreDocMock,
    registerPushServiceWorkerMock,
    touchCurrentWebPushEndpointMock,
    webPushStatusMock,
    notifyInfoMock,
} = vi.hoisted(() => {
    return {
        deactivateCurrentWebPushEndpointMock: vi.fn(),
        enableWebPushMock: vi.fn(),
        getDocMock: vi.fn(),
        hasCurrentWebPushSubscriptionMock: vi.fn(),
        firestoreDocMock: vi.fn((...args) => ({ path: args.join("/") })),
        registerPushServiceWorkerMock: vi.fn(),
        touchCurrentWebPushEndpointMock: vi.fn(),
        webPushStatusMock: vi.fn(),
        notifyInfoMock: vi.fn(),
    };
});

vi.mock("../push/webPush", () => {
    return {
        deactivateCurrentWebPushEndpoint: deactivateCurrentWebPushEndpointMock,
        enableWebPush: enableWebPushMock,
        hasCurrentWebPushSubscription: hasCurrentWebPushSubscriptionMock,
        registerPushServiceWorker: registerPushServiceWorkerMock,
        touchCurrentWebPushEndpoint: touchCurrentWebPushEndpointMock,
        webPushStatus: webPushStatusMock,
    };
});

vi.mock("../firebase", () => {
    return {
        db: { __mocked: true },
    };
});

vi.mock("firebase/firestore", () => {
    return {
        doc: firestoreDocMock,
        getDoc: getDocMock,
    };
});

vi.mock("../utils/notify", () => {
    return {
        notifyInfo: notifyInfoMock,
    };
});

describe("useWebPushLifecycle", () => {
    beforeEach(() => {
        deactivateCurrentWebPushEndpointMock.mockReset();
        enableWebPushMock.mockReset();
        getDocMock.mockReset();
        hasCurrentWebPushSubscriptionMock.mockReset();
        firestoreDocMock.mockClear();
        registerPushServiceWorkerMock.mockReset();
        touchCurrentWebPushEndpointMock.mockReset();
        webPushStatusMock.mockReset();
        notifyInfoMock.mockReset();
        webPushStatusMock.mockReturnValue({ featureEnabled: true, supported: true, permission: "default" });
        registerPushServiceWorkerMock.mockResolvedValue(null);
        touchCurrentWebPushEndpointMock.mockResolvedValue(true);
        hasCurrentWebPushSubscriptionMock.mockResolvedValue(true);
        getDocMock.mockResolvedValue({
            exists: () => true,
            data: () => ({ webPushEnabled: false }),
        });
        deactivateCurrentWebPushEndpointMock.mockResolvedValue({ endpointId: "ep_1" });
        enableWebPushMock.mockResolvedValue({ endpointId: "ep_new" });

        const listeners = new Map();
        Object.defineProperty(globalThis.navigator, "serviceWorker", {
            configurable: true,
            value: {
                addEventListener: vi.fn((type, handler) => {
                    listeners.set(type, handler);
                }),
                removeEventListener: vi.fn((type) => {
                    listeners.delete(type);
                }),
                __listeners: listeners,
            },
        });
    });

    it("registers the service worker and touches the current endpoint", async () => {
        renderHook(() => useWebPushLifecycle("user-1"));

        await act(async () => {
            await Promise.resolve();
        });

        expect(registerPushServiceWorkerMock).toHaveBeenCalledTimes(1);
        expect(touchCurrentWebPushEndpointMock).toHaveBeenCalledWith("user-1");
    });

    it("auto-enables push on a new device when account preference is already enabled", async () => {
        hasCurrentWebPushSubscriptionMock.mockResolvedValue(false);
        getDocMock.mockResolvedValue({
            exists: () => true,
            data: () => ({ webPushEnabled: true }),
        });

        renderHook(() => useWebPushLifecycle("user-1"));

        await act(async () => {
            await Promise.resolve();
            await Promise.resolve();
        });

        expect(getDocMock).toHaveBeenCalledTimes(1);
        expect(enableWebPushMock).toHaveBeenCalledWith("user-1");
    });

    it("re-subscribes when permission is granted but the current endpoint is missing", async () => {
        hasCurrentWebPushSubscriptionMock.mockResolvedValue(false);
        webPushStatusMock.mockReturnValue({ featureEnabled: true, supported: true, permission: "granted" });
        getDocMock.mockResolvedValue({
            exists: () => true,
            data: () => ({ webPushEnabled: true }),
        });

        renderHook(() => useWebPushLifecycle("user-1"));

        await act(async () => {
            await Promise.resolve();
            await Promise.resolve();
        });

        expect(enableWebPushMock).toHaveBeenCalledWith("user-1");
    });

    it("deactivates the previous endpoint on logout", async () => {
        const { rerender } = renderHook(({ uid }) => useWebPushLifecycle(uid), {
            initialProps: { uid: "user-1" },
        });

        await act(async () => {
            await Promise.resolve();
        });

        rerender({ uid: null });

        await act(async () => {
            await Promise.resolve();
        });

        expect(deactivateCurrentWebPushEndpointMock).toHaveBeenCalledWith("user-1", { unsubscribe: true });
    });

    it("shows a foreground info notification on push-received messages", async () => {
        renderHook(() => useWebPushLifecycle("user-1"));

        await act(async () => {
            await Promise.resolve();
        });

        const handler = navigator.serviceWorker.__listeners.get("message");
        expect(handler).toBeTypeOf("function");

        act(() => {
            handler({
                data: {
                    type: "push-received",
                    notification: {
                        title: "Poll opened",
                    },
                },
            });
        });

        expect(notifyInfoMock).toHaveBeenCalledWith("Poll opened");
    });

    it("touches the endpoint when the service worker reports subscription change", async () => {
        renderHook(() => useWebPushLifecycle("user-1"));

        await act(async () => {
            await Promise.resolve();
        });

        touchCurrentWebPushEndpointMock.mockClear();

        const handler = navigator.serviceWorker.__listeners.get("message");
        expect(handler).toBeTypeOf("function");

        act(() => {
            handler({
                data: {
                    type: "push-subscription-changed",
                },
            });
        });

        expect(touchCurrentWebPushEndpointMock).toHaveBeenCalledWith("user-1");
    });
});
