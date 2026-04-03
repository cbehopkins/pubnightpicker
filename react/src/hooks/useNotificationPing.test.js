// @vitest-environment jsdom

import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useNotificationPing } from "./useNotificationPing";

const {
    pingNotificationToolMock,
    clearNotificationPingMock,
} = vi.hoisted(() => {
    return {
        pingNotificationToolMock: vi.fn(),
        clearNotificationPingMock: vi.fn(),
    };
});

vi.mock("../dbtools/notificationPings", () => {
    return {
        pingNotificationTool: pingNotificationToolMock,
        clearNotificationPing: clearNotificationPingMock,
    };
});

describe("useNotificationPing", () => {
    beforeEach(() => {
        pingNotificationToolMock.mockReset();
        clearNotificationPingMock.mockReset();
    });

    it("starts in idle state with no last ping value", () => {
        const { result } = renderHook(() => useNotificationPing("poll-1", "create", 60000));

        expect(result.current.status).toBe("idle");
        expect(result.current.lastPingValue).toBeNull();
    });

    it("sets status to ok and stores ping value when acknowledged", async () => {
        pingNotificationToolMock.mockResolvedValue({
            acknowledged: true,
            timedOut: false,
            pingValue: 123,
        });

        const { result } = renderHook(() => useNotificationPing("poll-1", "create", 60000));

        await act(async () => {
            const response = await result.current.runPing();
            expect(response.pingValue).toBe(123);
        });

        expect(pingNotificationToolMock).toHaveBeenCalledWith("poll-1", "create", 60000);
        expect(result.current.status).toBe("ok");
        expect(result.current.lastPingValue).toBe(123);
    });

    it("sets status to timeout when acknowledgement is not received in time", async () => {
        pingNotificationToolMock.mockResolvedValue({
            acknowledged: false,
            timedOut: true,
            pingValue: 222,
        });

        const { result } = renderHook(() => useNotificationPing("poll-2", "complete", 60000));

        await act(async () => {
            await result.current.runPing();
        });

        expect(result.current.status).toBe("timeout");
        expect(result.current.lastPingValue).toBeNull();
    });

    it("sets status to error and rethrows when ping operation fails", async () => {
        const error = new Error("network down");
        pingNotificationToolMock.mockRejectedValue(error);

        const { result } = renderHook(() => useNotificationPing("poll-3", "complete", 60000));

        let caughtError = null;

        await act(async () => {
            try {
                await result.current.runPing();
            } catch (caught) {
                caughtError = caught;
            }
        });

        expect(caughtError).toBe(error);
        expect(result.current.status).toBe("error");
    });

    it("clears ping state and calls db clear helper", async () => {
        pingNotificationToolMock.mockResolvedValue({
            acknowledged: true,
            timedOut: false,
            pingValue: 999,
        });
        clearNotificationPingMock.mockResolvedValue(undefined);

        const { result } = renderHook(() => useNotificationPing("poll-4", "create", 60000));

        await act(async () => {
            await result.current.runPing();
        });

        await act(async () => {
            await result.current.clearPing();
        });

        expect(clearNotificationPingMock).toHaveBeenCalledWith("poll-4", "create");
        expect(result.current.status).toBe("idle");
        expect(result.current.lastPingValue).toBeNull();
    });

    it("reuses a single in-flight ping when runPing is called concurrently", async () => {
        let resolvePing;
        const deferredPing = new Promise((resolve) => {
            resolvePing = resolve;
        });

        pingNotificationToolMock.mockReturnValue(deferredPing);

        const { result } = renderHook(() => useNotificationPing("poll-5", "create", 60000));

        let firstPromise;
        let secondPromise;
        await act(async () => {
            firstPromise = result.current.runPing();
            secondPromise = result.current.runPing();
        });

        expect(pingNotificationToolMock).toHaveBeenCalledTimes(1);

        await act(async () => {
            resolvePing({
                acknowledged: true,
                timedOut: false,
                pingValue: 555,
            });
            const [firstResult, secondResult] = await Promise.all([firstPromise, secondPromise]);
            expect(firstResult).toEqual(secondResult);
        });

        expect(result.current.status).toBe("ok");
        expect(result.current.lastPingValue).toBe(555);
    });
});
