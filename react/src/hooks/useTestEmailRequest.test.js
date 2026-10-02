// @vitest-environment jsdom

import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import useTestEmailRequest, { resolveTestEmailAddress } from "./useTestEmailRequest";

const { docMock, onSnapshotMock, setDocMock } = vi.hoisted(() => ({
    docMock: vi.fn((_db, collection, id) => ({ path: `${collection}/${id}` })),
    onSnapshotMock: vi.fn(),
    setDocMock: vi.fn(),
}));

vi.mock("../firebase", () => ({ db: {} }));

vi.mock("firebase/firestore", () => ({
    doc: docMock,
    onSnapshot: onSnapshotMock,
    setDoc: setDocMock,
}));

function emit(snapshotHandler, data) {
    act(() => {
        snapshotHandler({ data: () => data });
    });
}

describe("useTestEmailRequest", () => {
    let snapshotHandler;
    const unsubscribe = vi.fn();

    beforeEach(() => {
        vi.useFakeTimers();
        docMock.mockClear();
        setDocMock.mockReset();
        setDocMock.mockResolvedValue(undefined);
        unsubscribe.mockClear();
        onSnapshotMock.mockReset();
        onSnapshotMock.mockImplementation((_ref, handler) => {
            snapshotHandler = handler;
            return unsubscribe;
        });
        vi.spyOn(crypto, "randomUUID").mockReturnValue("uuid-new");
    });

    afterEach(() => {
        vi.useRealTimers();
        vi.restoreAllMocks();
    });

    it("does not report sent for a stale matching req/ack on load", () => {
        const { result } = renderHook(() => useTestEmailRequest("u1"));

        emit(snapshotHandler, {
            email: "a@example.com",
            testEmailReq: "old",
            testEmailAck: "old",
        });

        expect(result.current.status).toBe("idle");
        expect(result.current.address).toBe("a@example.com");
    });

    it("writes a new UUID and reports sent when the ack matches it", async () => {
        const { result } = renderHook(() => useTestEmailRequest("u1"));
        emit(snapshotHandler, { email: "a@example.com", testEmailAck: "old" });

        await act(async () => {
            await result.current.sendTestEmail();
        });

        expect(setDocMock).toHaveBeenCalledWith(
            { path: "users/u1" },
            { testEmailReq: "uuid-new" },
            { merge: true },
        );
        expect(result.current.status).toBe("sending");

        emit(snapshotHandler, { email: "a@example.com", testEmailReq: "uuid-new", testEmailAck: "old" });
        expect(result.current.status).toBe("sending");

        emit(snapshotHandler, { email: "a@example.com", testEmailReq: "uuid-new", testEmailAck: "uuid-new" });
        expect(result.current.status).toBe("sent");
    });

    it("times out when no ack arrives", async () => {
        const { result } = renderHook(() => useTestEmailRequest("u1", 1000));

        await act(async () => {
            await result.current.sendTestEmail();
        });
        act(() => {
            vi.advanceTimersByTime(1000);
        });

        expect(result.current.status).toBe("timeout");
    });

    it("reports error when the request write fails", async () => {
        setDocMock.mockRejectedValue(new Error("denied"));
        vi.spyOn(console, "error").mockImplementation(() => undefined);
        const { result } = renderHook(() => useTestEmailRequest("u1"));

        await act(async () => {
            await result.current.sendTestEmail();
        });

        expect(result.current.status).toBe("error");
    });

    it("ignores repeat clicks while a request is pending", async () => {
        const { result } = renderHook(() => useTestEmailRequest("u1"));

        await act(async () => {
            await result.current.sendTestEmail();
            await result.current.sendTestEmail();
        });

        expect(setDocMock).toHaveBeenCalledTimes(1);
    });

    it("does not subscribe without a uid", () => {
        renderHook(() => useTestEmailRequest(null));

        expect(onSnapshotMock).not.toHaveBeenCalled();
    });
});

describe("resolveTestEmailAddress", () => {
    it("prefers notificationEmail and falls back to email", () => {
        expect(resolveTestEmailAddress({ notificationEmail: "n@x.com", email: "e@x.com" })).toBe("n@x.com");
        expect(resolveTestEmailAddress({ notificationEmail: " ", email: "e@x.com" })).toBe("e@x.com");
        expect(resolveTestEmailAddress(undefined)).toBeNull();
    });
});
