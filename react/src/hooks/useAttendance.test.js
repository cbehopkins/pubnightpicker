// @vitest-environment jsdom

import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
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
    streamLastOrdersMock,
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
        streamLastOrdersMock: vi.fn(),
    };
});

vi.mock("../utils/lastOrdersApi", () => ({
    requestLastOrders: requestLastOrdersMock,
    streamLastOrders: streamLastOrdersMock,
}));

vi.mock("react-redux", () => ({
    useSelector: (selector) => selector({ group: { groupId: "group-a" }, auth: { uid: "user-1" } }),
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
    afterEach(cleanup);

    beforeEach(() => {
        arrayRemoveMock.mockClear();
        arrayUnionMock.mockClear();
        deleteFieldMock.mockClear();
        docMock.mockClear();
        onSnapshotMock.mockReset();
        setDocMock.mockClear();
        updateDocMock.mockClear();
        requestLastOrdersMock.mockClear();
        streamLastOrdersMock.mockReset();
        requestLastOrdersMock.mockResolvedValue({
            meet_id: "poll-1",
            entries: {
                "pub-1": { can_come: ["user-1"], cannot_come: [], eta: { "user-1": "19:15" } },
            },
            participants: { "user-1": { id: "user-1", name: "Alice", photo_url: "https://example.test/alice.jpg" } },
            cursor: "cursor-1",
        });
        streamLastOrdersMock.mockImplementation((_path, options) => new Promise((resolve) => {
            options.signal.addEventListener("abort", resolve, { once: true });
        }));
        runTransactionMock.mockReset();
        runTransactionMock.mockImplementation(async (_db, callback) => callback({
            get: vi.fn(async () => ({ exists: () => true })),
            set: vi.fn(async (ref, payload) => updateDocMock(ref, payload)),
        }));
    });

    it("loads a complete attendance snapshot and applies Group stream replacements", async () => {
        const { result } = renderHook(() => useAttendance("poll-1"));

        await waitFor(() => expect(result.current[8]).toBe(false));
        await waitFor(() => expect(streamLastOrdersMock).toHaveBeenCalledTimes(1));

        expect(requestLastOrdersMock).toHaveBeenCalledWith("/api/meet/poll-1/attendance", expect.objectContaining({ method: "GET" }));
        expect(result.current[0]).toEqual({
            "pub-1": {
                canCome: ["user-1"],
                cannotCome: [],
                eta: { "user-1": "19:15" },
            },
        });
        expect(result.current[7]["user-1"]).toEqual({ uid: "user-1", name: "Alice", photoUrl: "https://example.test/alice.jpg" });
        expect(onSnapshotMock).not.toHaveBeenCalled();

        const streamOptions = streamLastOrdersMock.mock.calls[0][1];
        act(() => streamOptions.onmessage({
            event: "attendance_snapshots",
            data: JSON.stringify({
                cursor: "cursor-2",
                meets: [{
                    meet_id: "poll-1",
                    cursor: "meet-cursor-2",
                    entries: { "pub-1": { can_come: ["user-2"], cannot_come: ["user-1"], eta: {} } },
                    participants: { "user-1": { id: "user-1", name: "Alice" }, "user-2": { id: "user-2", name: "Bob" } },
                }],
            }),
        }));

        await waitFor(() => expect(result.current[0]["pub-1"].cannotCome).toEqual(["user-1"]));
        expect(result.current[0]["pub-1"].canCome).toEqual(["user-2"]);
        expect(result.current[7]["user-2"].name).toBe("Bob");
    });

    it("shares one bounded Group stream across displayed Meets", async () => {
        const first = renderHook(() => useAttendance("meet-b"));
        const second = renderHook(() => useAttendance("meet-a"));
        await waitFor(() => expect(streamLastOrdersMock).toHaveBeenCalledTimes(1));

        expect(streamLastOrdersMock).toHaveBeenCalledWith("api/meets/group-a/attendance/stream", expect.objectContaining({
            query: [["meet_id", "meet-a"], ["meet_id", "meet-b"]],
        }));
        first.unmount();
        second.unmount();
    });

    it("sets and withdraws the caller's Venue response through Last Orders", async () => {
        const { result } = renderHook(() => useAttendance("meet-1"));

        await act(async () => {
            await result.current[1]("venue-1", "user-1", "canCome");
            await result.current[2]("venue-1", "user-1");
        });

        expect(requestLastOrdersMock).toHaveBeenCalledWith("/api/meet/meet-1/attendance/venue-1", {
            method: "PUT",
            body: { status: "attending" },
            signal: expect.any(AbortSignal),
        });
        expect(requestLastOrdersMock).toHaveBeenCalledWith("/api/meet/meet-1/attendance/venue-1", {
            method: "DELETE",
            signal: expect.any(AbortSignal),
        });
        expect(updateDocMock).not.toHaveBeenCalled();
    });

    it("does not subscribe when disabled", () => {
        const { result } = renderHook(() => useAttendance("poll-1", false));

        expect(onSnapshotMock).not.toHaveBeenCalled();
        expect(streamLastOrdersMock).not.toHaveBeenCalled();
        expect(result.current[0]).toEqual({});
    });

    it("clears attendance and profiles when disabled after loading", async () => {
        const { result, rerender } = renderHook(({ enabled }) => useAttendance("poll-1", enabled), { initialProps: { enabled: true } });
        await waitFor(() => expect(result.current[7]["user-1"]).toBeDefined());

        rerender({ enabled: false });
        expect(result.current[0]).toEqual({});
        expect(result.current[7]).toEqual({});
        expect(result.current[8]).toBe(false);
    });

    it("switches and clears attendance through the API", async () => {
        onSnapshotMock.mockImplementation(() => () => undefined);

        const { result } = renderHook(() => useAttendance("poll-1"));

        await act(async () => {
            await result.current[1]("pub-1", "user-1", "canCome");
        });

        expect(requestLastOrdersMock).toHaveBeenCalledWith("/api/meet/poll-1/attendance/pub-1", {
            method: "PUT", body: { status: "attending" }, signal: expect.any(AbortSignal),
        });

        await act(async () => {
            await result.current[2]("pub-1", "user-1");
        });

        expect(requestLastOrdersMock).toHaveBeenLastCalledWith("/api/meet/poll-1/attendance/pub-1", { method: "DELETE", signal: expect.any(AbortSignal) });
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
            method: "PUT", body: { status: "not_attending" }, signal: expect.any(AbortSignal),
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
            method: "PUT", body: { status: "attending" }, signal: expect.any(AbortSignal),
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
        const log = vi.spyOn(console, "log").mockImplementation(() => {});
        requestLastOrdersMock
            .mockResolvedValueOnce({ meet_id: "poll-1", entries: {}, participants: {}, cursor: "cursor-1" })
            .mockRejectedValueOnce(new Error("The requested Meet was not found."));

        const { result } = renderHook(() => useAttendance("poll-1"));

        await expect(act(async () => {
            await result.current[1]("pub-1", "user-1", "canCome");
        })).rejects.toThrow("The requested Meet was not found.");
        expect(setDocMock).not.toHaveBeenCalled();
        expect(updateDocMock).not.toHaveBeenCalled();
        expect(log).toHaveBeenCalled();
        log.mockRestore();
    });

    it("logs a failed Venue write and reloads the observed attendance", async () => {
        const log = vi.spyOn(console, "log").mockImplementation(() => {});
        const failure = new Error("connection lost");
        const { result } = renderHook(() => useAttendance("poll-1"));
        await waitFor(() => expect(result.current[8]).toBe(false));
        requestLastOrdersMock.mockRejectedValueOnce(failure).mockResolvedValueOnce({
            meet_id: "poll-1",
            entries: { "pub-1": { can_come: [], cannot_come: ["user-1"], eta: {} } },
            participants: {},
            cursor: "cursor-2",
        });

        let writeError;
        await act(async () => {
            try {
                await result.current[1]("pub-1", "user-1", "cannotCome");
            } catch (error) {
                writeError = error;
            }
        });
        expect(writeError).toBe(failure);
        await waitFor(() => expect(result.current[0]["pub-1"].cannotCome).toEqual(["user-1"]));
        expect(log).toHaveBeenCalledWith(failure);
        expect(requestLastOrdersMock).toHaveBeenLastCalledWith("/api/meet/poll-1/attendance", expect.objectContaining({ method: "GET" }));
        log.mockRestore();
    });

    it("does not replace a newer stream update with a failed-write refresh", async () => {
        const log = vi.spyOn(console, "log").mockImplementation(() => {});
        const { result } = renderHook(() => useAttendance("poll-1"));
        await waitFor(() => expect(streamLastOrdersMock).toHaveBeenCalledTimes(1));
        let resolveRefresh;
        requestLastOrdersMock.mockRejectedValueOnce(new Error("connection lost")).mockImplementationOnce(() => new Promise((resolve) => {
            resolveRefresh = resolve;
        }));

        await expect(act(async () => result.current[2]("pub-1", "user-1"))).rejects.toThrow("connection lost");
        const streamOptions = streamLastOrdersMock.mock.calls[0][1];
        act(() => streamOptions.onmessage({
            event: "attendance_snapshots",
            data: JSON.stringify({
                cursor: "group-2",
                meets: [{ meet_id: "poll-1", cursor: "cursor-2", entries: { "pub-1": { can_come: [], cannot_come: ["user-1"], eta: {} } }, participants: {} }],
            }),
        }));
        await act(async () => resolveRefresh({
            meet_id: "poll-1", cursor: "cursor-1", entries: { "pub-1": { can_come: ["user-1"], cannot_come: [], eta: {} } }, participants: {},
        }));
        expect(result.current[0]["pub-1"].cannotCome).toEqual(["user-1"]);
        log.mockRestore();
    });

    it("does not replace a newer refresh with an older snapshot request", async () => {
        const log = vi.spyOn(console, "log").mockImplementation(() => {});
        let resolveInitial;
        requestLastOrdersMock.mockImplementationOnce(() => new Promise((resolve) => { resolveInitial = resolve; }));
        const { result } = renderHook(() => useAttendance("poll-1"));
        requestLastOrdersMock.mockRejectedValueOnce(new Error("connection lost")).mockResolvedValueOnce({
            meet_id: "poll-1", cursor: "cursor-2", entries: { "pub-1": { can_come: [], cannot_come: ["user-1"], eta: {} } }, participants: {},
        });

        await act(async () => { await result.current[1]("pub-1", "user-1", "cannotCome").catch(() => {}); });
        await act(async () => resolveInitial({
            meet_id: "poll-1", cursor: "cursor-1", entries: { "pub-1": { can_come: ["user-1"], cannot_come: [], eta: {} } }, participants: {},
        }));
        expect(result.current[0]["pub-1"].cannotCome).toEqual(["user-1"]);
        log.mockRestore();
    });

    it("clears the last snapshot when a refresh confirms access is lost", async () => {
        const log = vi.spyOn(console, "log").mockImplementation(() => {});
        const { result } = renderHook(() => useAttendance("poll-1"));
        await waitFor(() => expect(result.current[7]["user-1"]).toBeDefined());
        requestLastOrdersMock.mockRejectedValueOnce(new Error("connection lost")).mockRejectedValueOnce({ status: 403, message: "Access denied" });

        await act(async () => { await result.current[2]("pub-1", "user-1").catch(() => {}); });
        await waitFor(() => expect(result.current[9]).toBe("Access denied"));
        expect(result.current[0]).toEqual({});
        expect(result.current[7]).toEqual({});
        log.mockRestore();
    });

    it("times out an unanswered Venue write and refreshes attendance", async () => {
        const log = vi.spyOn(console, "log").mockImplementation(() => {});
        const { result } = renderHook(() => useAttendance("poll-1"));
        await waitFor(() => expect(result.current[8]).toBe(false));
        vi.useFakeTimers();
        requestLastOrdersMock.mockImplementationOnce((_path, options) => new Promise((_resolve, reject) => {
            options.signal.addEventListener("abort", () => reject(options.signal.reason), { once: true });
        }));

        let outcome;
        act(() => {
            outcome = result.current[1]("pub-1", "user-1", "cannotCome").catch((error) => error);
        });
        await act(async () => { await vi.advanceTimersByTimeAsync(10000); });
        expect((await outcome).name).toBe("AbortError");
        expect(log).toHaveBeenCalledOnce();
        expect(requestLastOrdersMock).toHaveBeenLastCalledWith("/api/meet/poll-1/attendance", expect.objectContaining({ method: "GET" }));
        vi.useRealTimers();
        log.mockRestore();
    });

    it("withdrawal uses the API so the backend can clear the caller's ETA", async () => {
        onSnapshotMock.mockImplementation(() => () => undefined);

        const { result } = renderHook(() => useAttendance("poll-1"));

        await act(async () => {
            await result.current[2]("pub-1", "user-1");
        });

        expect(requestLastOrdersMock).toHaveBeenCalledWith("/api/meet/poll-1/attendance/pub-1", { method: "DELETE", signal: expect.any(AbortSignal) });
        expect(updateDocMock).not.toHaveBeenCalled();
    });

    it("setting cannotCome uses the API so the backend clears the caller's ETA", async () => {
        onSnapshotMock.mockImplementation(() => () => undefined);

        const { result } = renderHook(() => useAttendance("poll-1"));

        await act(async () => {
            await result.current[1]("pub-1", "user-1", "cannotCome");
        });

        expect(requestLastOrdersMock).toHaveBeenCalledWith("/api/meet/poll-1/attendance/pub-1", {
            method: "PUT", body: { status: "not_attending" }, signal: expect.any(AbortSignal),
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
            method: "PUT", body: { status: "attending" }, signal: expect.any(AbortSignal),
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
