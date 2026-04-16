// @vitest-environment jsdom

import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import useWebPushSettings from "./useWebPushSettings";

const {
    deactivateCurrentWebPushEndpointMock,
    enableWebPushMock,
    webPushStatusMock,
} = vi.hoisted(() => {
    return {
        deactivateCurrentWebPushEndpointMock: vi.fn(),
        enableWebPushMock: vi.fn(),
        webPushStatusMock: vi.fn(),
    };
});

vi.mock("../push/webPush", () => {
    return {
        deactivateCurrentWebPushEndpoint: deactivateCurrentWebPushEndpointMock,
        enableWebPush: enableWebPushMock,
        webPushStatus: webPushStatusMock,
    };
});

describe("useWebPushSettings", () => {
    beforeEach(() => {
        deactivateCurrentWebPushEndpointMock.mockReset();
        enableWebPushMock.mockReset();
        webPushStatusMock.mockReset();
        webPushStatusMock.mockReturnValue({
            supported: true,
            featureEnabled: true,
            permission: "default",
        });
    });

    it("enables push and updates local state on success", async () => {
        enableWebPushMock.mockResolvedValue({ endpointId: "ep_123" });

        const { result } = renderHook(() => useWebPushSettings("user-1", false));

        await act(async () => {
            const success = await result.current.enable();
            expect(success).toBe(true);
        });

        expect(enableWebPushMock).toHaveBeenCalledWith("user-1");
        expect(result.current.enabled).toBe(true);
        expect(result.current.error).toBe("");
    });

    it("surfaces an error when enabling fails", async () => {
        enableWebPushMock.mockRejectedValue(new Error("permission denied"));

        const { result } = renderHook(() => useWebPushSettings("user-1", false));

        await act(async () => {
            const success = await result.current.enable();
            expect(success).toBe(false);
        });

        expect(result.current.enabled).toBe(false);
        expect(result.current.error).toBe("permission denied");
    });

    it("disables push and clears local state on success", async () => {
        deactivateCurrentWebPushEndpointMock.mockResolvedValue({ endpointId: "ep_123" });

        const { result } = renderHook(() => useWebPushSettings("user-1", true));

        await act(async () => {
            const success = await result.current.disable();
            expect(success).toBe(true);
        });

        expect(deactivateCurrentWebPushEndpointMock).toHaveBeenCalledWith("user-1", { unsubscribe: true });
        expect(result.current.enabled).toBe(false);
        expect(result.current.error).toBe("");
    });

    it("returns a user-facing error when uid is missing", async () => {
        const { result } = renderHook(() => useWebPushSettings(null, false));

        await act(async () => {
            const success = await result.current.enable();
            expect(success).toBe(false);
        });

        expect(enableWebPushMock).not.toHaveBeenCalled();
        expect(result.current.error).toBe("You must be logged in to enable web push");
    });

    it("syncs enabled state when initialEnabled changes after mount", async () => {
        const { result, rerender } = renderHook(
            ({ uid, initialEnabled }) => useWebPushSettings(uid, initialEnabled),
            {
                initialProps: { uid: "user-1", initialEnabled: false },
            },
        );

        expect(result.current.enabled).toBe(false);

        rerender({ uid: "user-1", initialEnabled: true });

        await act(async () => {
            await Promise.resolve();
        });

        expect(result.current.enabled).toBe(true);
    });
});
