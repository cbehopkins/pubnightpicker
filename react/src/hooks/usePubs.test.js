// @vitest-environment jsdom

import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import usePubs from "./usePubs";

const {
  collectionMock,
  onSnapshotMock,
} = vi.hoisted(() => {
  return {
    collectionMock: vi.fn(() => ({ id: "pubs-collection" })),
    onSnapshotMock: vi.fn(),
  };
});

vi.mock("../firebase", () => {
  return {
    db: {},
  };
});

vi.mock("firebase/firestore", () => {
  return {
    collection: collectionMock,
    onSnapshot: onSnapshotMock,
  };
});

describe("usePubs", () => {
  beforeEach(() => {
    collectionMock.mockClear();
    onSnapshotMock.mockReset();
  });

  it("defaults missing venueType to pub for added documents", () => {
    let snapshotHandler;
    onSnapshotMock.mockImplementation((_, handler) => {
      snapshotHandler = handler;
      return () => undefined;
    });

    const { result } = renderHook(() => usePubs());

    act(() => {
      snapshotHandler({
        docChanges: () => [
          {
            type: "added",
            doc: {
              id: "venue-1",
              data: () => ({ name: "Cambridge Beer Festival" }),
            },
          },
        ],
      });
    });

    expect(result.current["venue-1"]).toEqual({
      name: "Cambridge Beer Festival",
      venueType: "pub",
    });
  });

  it("preserves explicit venueType when present", () => {
    let snapshotHandler;
    onSnapshotMock.mockImplementation((_, handler) => {
      snapshotHandler = handler;
      return () => undefined;
    });

    const { result } = renderHook(() => usePubs());

    act(() => {
      snapshotHandler({
        docChanges: () => [
          {
            type: "added",
            doc: {
              id: "venue-2",
              data: () => ({ name: "Bistro 12", venueType: "restaurant" }),
            },
          },
        ],
      });
    });

    expect(result.current["venue-2"]).toEqual({
      name: "Bistro 12",
      venueType: "restaurant",
    });
  });
});
