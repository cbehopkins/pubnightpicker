// @vitest-environment jsdom

import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import useAttendance from "./useAttendance";

const {
    arrayRemoveMock,
    arrayUnionMock,
    deleteFieldMock,
    docMock,
    onSnapshotMock,
    setDocMock,
    updateDocMock,
    runTransactionMock,
    requestLastOrdersMock,
} = vi.hoisted(() => {
    const updateDocMock = vi.fn(async () => undefined);
    const runTransactionMock = vi.fn(async (_db, callback) => callback({
        get: vi.fn(async () => ({ exists: () => true })),
        set: vi.fn(async (ref, payload) => updateDocMock(ref, payload)),
    }));
    return {
        arrayRemoveMock: vi.fn((value) => `arrayRemove:${value}`),
        arrayUnionMock: vi.fn((value) => `arrayUnion:${value}`),
        deleteFieldMock: vi.fn(() => "deleteField"),
        docMock: vi.fn(() => ({ id: "attendance-doc-ref" })),
        onSnapshotMock: vi.fn(),
        setDocMock: vi.fn(async () => undefined),
        updateDocMock,
        runTransactionMock,
        requestLastOrdersMock: vi.fn(async () => undefined),
    };
});

vi.mock("../utils/lastOrdersApi", () => ({
    requestLastOrders: requestLastOrdersMock,
}));

vi.mock("../firebase", () => {
    return {
        db: {},
    };
});

vi.mock("firebase/firestore", () => {
    return {
        arrayRemove: arrayRemoveMock,
        arrayUnion: arrayUnionMock,
        deleteField: deleteFieldMock,
        doc: docMock,
        onSnapshot: onSnapshotMock,
        setDoc: setDocMock,
        updateDoc: updateDocMock,
        runTransaction: runTransactionMock,
    };
});

describe("useAttendance", () => {
    beforeEach(() => {
        arrayRemoveMock.mockClear();
        arrayUnionMock.mockClear();
        deleteFieldMock.mockClear();
        docMock.mockClear();
        onSnapshotMock.mockReset();
        setDocMock.mockClear();
        updateDocMock.mockClear();
        requestLastOrdersMock.mockClear();
        runTransactionMock.mockReset();
        runTransactionMock.mockImplementation(async (_db, callback) => callback({
            get: vi.fn(async () => ({ exists: () => true })),
            set: vi.fn(async (ref, payload) => updateDocMock(ref, payload)),
        }));
    });

    it("subscribes to attendance and replaces state from snapshots", () => {
        let snapshotHandler;
        onSnapshotMock.mockImplementation((docRef, handler) => {
            snapshotHandler = handler;
            return () => undefined;
        });

        const { result } = renderHook(() => useAttendance("poll-1"));

        act(() => {
            snapshotHandler({
                data: () => ({
                    "pub-1": {
                        canCome: ["user-1"],
                        cannotCome: [],
                    },
                }),
            });
        });

        expect(result.current[0]).toEqual({
            "pub-1": {
                canCome: ["user-1"],
                cannotCome: [],
            },
        });
    });

    it("sets and withdraws the caller's Venue response through Last Orders", async () => {
        onSnapshotMock.mockImplementation(() => () => undefined);

        const { result } = renderHook(() => useAttendance("meet-1"));

        await act(async () => {
            await result.current[1]("venue-1", "user-1", "canCome");
            await result.current[2]("venue-1", "user-1");
        });

        expect(requestLastOrdersMock).toHaveBeenNthCalledWith(1, "/api/meet/meet-1/attendance/venue-1", {
            method: "PUT",
            body: { status: "attending" },
        });
        expect(requestLastOrdersMock).toHaveBeenNthCalledWith(2, "/api/meet/meet-1/attendance/venue-1", {
            method: "DELETE",
        });
        expect(updateDocMock).not.toHaveBeenCalled();
    });

    it("subscribes once and does not churn subscriptions after snapshot updates", () => {
        const unsubscribeMock = vi.fn();
        let snapshotHandler;

        onSnapshotMock.mockImplementation((_, handler) => {
            snapshotHandler = handler;
            return unsubscribeMock;
        });

        renderHook(() => useAttendance("poll-1"));

        act(() => {
            snapshotHandler({
                data: () => ({
                    "pub-1": {
                        canCome: ["user-1"],
                        cannotCome: [],
                    },
                }),
            });
        });

        act(() => {
            snapshotHandler({
                data: () => ({
                    "pub-1": {
                        canCome: ["user-1", "user-2"],
                        cannotCome: [],
                    },
                }),
            });
        });

        expect(onSnapshotMock).toHaveBeenCalledTimes(1);
        expect(unsubscribeMock).not.toHaveBeenCalled();
    });

    it("does not subscribe when disabled", () => {
        const { result } = renderHook(() => useAttendance("poll-1", false));

        expect(onSnapshotMock).not.toHaveBeenCalled();
        expect(result.current[0]).toEqual({});
    });

    it("switches and clears attendance through the API", async () => {
        onSnapshotMock.mockImplementation(() => () => undefined);

        const { result } = renderHook(() => useAttendance("poll-1"));

        await act(async () => {
            await result.current[1]("pub-1", "user-1", "canCome");
        });

        expect(requestLastOrdersMock).toHaveBeenCalledWith("/api/meet/poll-1/attendance/pub-1", {
            method: "PUT", body: { status: "attending" },
        });

        await act(async () => {
            await result.current[2]("pub-1", "user-1");
        });

        expect(requestLastOrdersMock).toHaveBeenLastCalledWith("/api/meet/poll-1/attendance/pub-1", { method: "DELETE" });
        expect(updateDocMock).not.toHaveBeenCalled();
    });

    it("switches from canCome to cannotCome through the API", async () => {
        onSnapshotMock.mockImplementation(() => () => undefined);

        const { result } = renderHook(() => useAttendance("poll-1"));

        // First set canCome
        await act(async () => {
            await result.current[1]("pub-1", "user-1", "canCome");
        });

        updateDocMock.mockClear();

        // Now switch to cannotCome — must atomically remove canCome and add cannotCome
        await act(async () => {
            await result.current[1]("pub-1", "user-1", "cannotCome");
        });

        expect(requestLastOrdersMock).toHaveBeenLastCalledWith("/api/meet/poll-1/attendance/pub-1", {
            method: "PUT", body: { status: "not_attending" },
        });
        expect(updateDocMock).not.toHaveBeenCalled();
    });

    it("switches from cannotCome to canCome through the API", async () => {
        onSnapshotMock.mockImplementation(() => () => undefined);

        const { result } = renderHook(() => useAttendance("poll-1"));

        await act(async () => {
            await result.current[1]("pub-1", "user-1", "cannotCome");
        });

        updateDocMock.mockClear();

        await act(async () => {
            await result.current[1]("pub-1", "user-1", "canCome");
        });

        expect(requestLastOrdersMock).toHaveBeenLastCalledWith("/api/meet/poll-1/attendance/pub-1", {
            method: "PUT", body: { status: "attending" },
        });
        expect(updateDocMock).not.toHaveBeenCalled();
    });

    it("sets attendance for multiple pubs in one update", async () => {
        onSnapshotMock.mockImplementation(() => () => undefined);

        const { result } = renderHook(() => useAttendance("poll-1"));

        await act(async () => {
            await result.current[3](["pub-1", "pub-2"], "user-1", "canCome");
        });

        expect(updateDocMock).toHaveBeenCalledWith(
            { id: "attendance-doc-ref" },
            {
                "pub-1": { canCome: "arrayUnion:user-1", cannotCome: "arrayRemove:user-1" },
                "pub-2": { canCome: "arrayUnion:user-1", cannotCome: "arrayRemove:user-1" },
            },
        );
    });

    it("does not update when bulk helper receives no pubs", async () => {
        onSnapshotMock.mockImplementation(() => () => undefined);

        const { result } = renderHook(() => useAttendance("poll-1"));

        await act(async () => {
            await result.current[3]([], "user-1", "cannotCome");
        });

        expect(updateDocMock).not.toHaveBeenCalled();
    });

    it("sets global attendance sentinel and existing pubs in one update", async () => {
        onSnapshotMock.mockImplementation(() => () => undefined);

        const { result } = renderHook(() => useAttendance("poll-1"));

        await act(async () => {
            await result.current[4](["pub-1", "pub-2"], "user-1", "canCome");
        });

        expect(updateDocMock).toHaveBeenCalledWith(
            { id: "attendance-doc-ref" },
            {
                "any": { canCome: "arrayUnion:user-1", cannotCome: "arrayRemove:user-1" },
                "pub-1": { canCome: "arrayUnion:user-1", cannotCome: "arrayRemove:user-1" },
                "pub-2": { canCome: "arrayUnion:user-1", cannotCome: "arrayRemove:user-1" },
            },
        );
    });

    it("propagates API errors when the Meet has been deleted", async () => {
        onSnapshotMock.mockImplementation(() => () => undefined);
        requestLastOrdersMock.mockRejectedValueOnce(new Error("The requested Meet was not found."));

        const { result } = renderHook(() => useAttendance("poll-1"));

        await expect(act(async () => {
            await result.current[1]("pub-1", "user-1", "canCome");
        })).rejects.toThrow("The requested Meet was not found.");
        expect(setDocMock).not.toHaveBeenCalled();
        expect(updateDocMock).not.toHaveBeenCalled();
    });

    it("withdrawal uses the API so the backend can clear the caller's ETA", async () => {
        onSnapshotMock.mockImplementation(() => () => undefined);

        const { result } = renderHook(() => useAttendance("poll-1"));

        await act(async () => {
            await result.current[2]("pub-1", "user-1");
        });

        expect(requestLastOrdersMock).toHaveBeenCalledWith("/api/meet/poll-1/attendance/pub-1", { method: "DELETE" });
        expect(updateDocMock).not.toHaveBeenCalled();
    });

    it("setting cannotCome uses the API so the backend clears the caller's ETA", async () => {
        onSnapshotMock.mockImplementation(() => () => undefined);

        const { result } = renderHook(() => useAttendance("poll-1"));

        await act(async () => {
            await result.current[1]("pub-1", "user-1", "cannotCome");
        });

        expect(requestLastOrdersMock).toHaveBeenCalledWith("/api/meet/poll-1/attendance/pub-1", {
            method: "PUT", body: { status: "not_attending" },
        });
        expect(updateDocMock).not.toHaveBeenCalled();
    });

    it("setting canCome leaves ETA management to the backend without a Firestore write", async () => {
        onSnapshotMock.mockImplementation(() => () => undefined);

        const { result } = renderHook(() => useAttendance("poll-1"));

        await act(async () => {
            await result.current[1]("pub-1", "user-1", "canCome");
        });

        expect(requestLastOrdersMock).toHaveBeenCalledWith("/api/meet/poll-1/attendance/pub-1", {
            method: "PUT", body: { status: "attending" },
        });
        expect(updateDocMock).not.toHaveBeenCalled();
    });

    it("setEta writes the HH:MM string to the eta sub-map", async () => {
        onSnapshotMock.mockImplementation(() => () => undefined);

        const { result } = renderHook(() => useAttendance("poll-1"));

        await act(async () => {
            await result.current[5]("pub-1", "user-1", "18:30");
        });

        expect(updateDocMock).toHaveBeenCalledWith(
            { id: "attendance-doc-ref" },
            { "pub-1": { eta: { "user-1": "18:30" } } },
        );
    });

    it("clearEta deletes the eta key for the user", async () => {
        onSnapshotMock.mockImplementation(() => () => undefined);

        const { result } = renderHook(() => useAttendance("poll-1"));

        await act(async () => {
            await result.current[6]("pub-1", "user-1");
        });

        expect(updateDocMock).toHaveBeenCalledWith(
            { id: "attendance-doc-ref" },
            { "pub-1": { eta: { "user-1": "deleteField" } } },
        );
    });
});
