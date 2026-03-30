// @vitest-environment jsdom

import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import useVotes from "./useVotes";

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
    docMock: vi.fn(() => ({ id: "votes-doc-ref" })),
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

describe("useVotes", () => {
  beforeEach(() => {
    arrayRemoveMock.mockClear();
    arrayUnionMock.mockClear();
    docMock.mockClear();
    onSnapshotMock.mockReset();
    updateDocMock.mockClear();
  });

  it("replaces vote state from snapshots", () => {
    let snapshotHandler;
    onSnapshotMock.mockImplementation((_, handler) => {
      snapshotHandler = handler;
      return () => undefined;
    });

    const { result } = renderHook(() => useVotes("poll-1"));

    act(() => {
      snapshotHandler({
        data: () => ({
          "pub-1": ["user-1", "user-2"],
          any: ["user-3"],
        }),
      });
    });

    expect(result.current[0]).toEqual({
      "pub-1": ["user-1", "user-2"],
      any: ["user-3"],
    });

    act(() => {
      snapshotHandler({
        data: () => ({
          any: ["user-3"],
        }),
      });
    });

    expect(result.current[0]).toEqual({
      any: ["user-3"],
    });
  });

  it("subscribes once and does not churn subscriptions after snapshot updates", () => {
    const unsubscribeMock = vi.fn();
    let snapshotHandler;
    onSnapshotMock.mockImplementation((_, handler) => {
      snapshotHandler = handler;
      return unsubscribeMock;
    });

    renderHook(() => useVotes("poll-1"));

    act(() => {
      snapshotHandler({
        data: () => ({
          "pub-1": ["user-1"],
        }),
      });
    });

    act(() => {
      snapshotHandler({
        data: () => ({
          "pub-1": ["user-1", "user-2"],
        }),
      });
    });

    expect(onSnapshotMock).toHaveBeenCalledTimes(1);
    expect(unsubscribeMock).not.toHaveBeenCalled();
  });

  it("writes votes using arrayUnion and arrayRemove", async () => {
    onSnapshotMock.mockImplementation(() => () => undefined);

    const { result } = renderHook(() => useVotes("poll-1"));

    await act(async () => {
      await result.current[1]("pub-1", "user-1");
    });

    expect(updateDocMock).toHaveBeenCalledWith(
      { id: "votes-doc-ref" },
      {
        "pub-1": "arrayUnion:user-1",
      },
    );

    await act(async () => {
      await result.current[2]("pub-1", "user-1");
    });

    expect(updateDocMock).toHaveBeenCalledWith(
      { id: "votes-doc-ref" },
      {
        "pub-1": "arrayRemove:user-1",
      },
    );
  });
});
