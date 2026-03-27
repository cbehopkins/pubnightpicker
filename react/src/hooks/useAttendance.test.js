// @vitest-environment jsdom

import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import useAttendance from "./useAttendance";

const {
    arrayRemoveMock,
    arrayUnionMock,
    docMock,
    onSnapshotMock,
    updateDocMock,
} = vi.hoisted(() => {
    return {
        arrayRemoveMock: vi.fn((value) => `arrayRemove:${value}`),
        arrayUnionMock: vi.fn((value) => `arrayUnion:${value}`),
        docMock: vi.fn(() => ({ id: "attendance-doc-ref" })),
        onSnapshotMock: vi.fn(),
        updateDocMock: vi.fn(async () => undefined),
    };
});

vi.mock("../firebase", () => {
    return {
        db: {},
    };
});

vi.mock("firebase/firestore", () => {
    return {
        arrayRemove: arrayRemoveMock,
        arrayUnion: arrayUnionMock,
        doc: docMock,
        onSnapshot: onSnapshotMock,
        updateDoc: updateDocMock,
    };
});

describe("useAttendance", () => {
    beforeEach(() => {
        arrayRemoveMock.mockClear();
        arrayUnionMock.mockClear();
        docMock.mockClear();
        onSnapshotMock.mockReset();
        updateDocMock.mockClear();
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

    it("switches and clears attendance with mutually exclusive updates", async () => {
        onSnapshotMock.mockImplementation(() => () => undefined);

        const { result } = renderHook(() => useAttendance("poll-1"));

        await act(async () => {
            await result.current[1]("pub-1", "user-1", "canCome");
        });

        expect(updateDocMock).toHaveBeenCalledWith(
            { id: "attendance-doc-ref" },
            {
                "pub-1.canCome": "arrayUnion:user-1",
                "pub-1.cannotCome": "arrayRemove:user-1",
            },
        );

        await act(async () => {
            await result.current[2]("pub-1", "user-1");
        });

        expect(updateDocMock).toHaveBeenCalledWith(
            { id: "attendance-doc-ref" },
            {
                "pub-1.canCome": "arrayRemove:user-1",
                "pub-1.cannotCome": "arrayRemove:user-1",
            },
        );
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
                "pub-1.canCome": "arrayUnion:user-1",
                "pub-1.cannotCome": "arrayRemove:user-1",
                "pub-2.canCome": "arrayUnion:user-1",
                "pub-2.cannotCome": "arrayRemove:user-1",
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
});
