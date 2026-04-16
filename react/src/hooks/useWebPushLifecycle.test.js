// @vitest-environment jsdom

import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import useWebPushLifecycle from "./useWebPushLifecycle";

const {
    deactivateCurrentWebPushEndpointMock,
    registerPushServiceWorkerMock,
    touchCurrentWebPushEndpointMock,
    webPushStatusMock,
    notifyInfoMock,
} = vi.hoisted(() => {
    return {
        deactivateCurrentWebPushEndpointMock: vi.fn(),
        registerPushServiceWorkerMock: vi.fn(),
        touchCurrentWebPushEndpointMock: vi.fn(),
        webPushStatusMock: vi.fn(),
        notifyInfoMock: vi.fn(),
    };
});

vi.mock("../push/webPush", () => {
    return {
        deactivateCurrentWebPushEndpoint: deactivateCurrentWebPushEndpointMock,
        registerPushServiceWorker: registerPushServiceWorkerMock,
        touchCurrentWebPushEndpoint: touchCurrentWebPushEndpointMock,
        webPushStatus: webPushStatusMock,
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
        registerPushServiceWorkerMock.mockReset();
        touchCurrentWebPushEndpointMock.mockReset();
        webPushStatusMock.mockReset();
        notifyInfoMock.mockReset();
        webPushStatusMock.mockReturnValue({ featureEnabled: true, supported: true });
        registerPushServiceWorkerMock.mockResolvedValue(null);
        touchCurrentWebPushEndpointMock.mockResolvedValue(true);
        deactivateCurrentWebPushEndpointMock.mockResolvedValue({ endpointId: "ep_1" });

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
});
