// @vitest-environment jsdom

import { renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import usePolls, { useActiveMeets, useCurrentMeets, usePastMeets } from "./usePolls";

const { records, completedRecords, whereMock, getDocsMock, getDocMock, limitMock, startAfterMock } = vi.hoisted(() => ({
  records: {
    "meet-1": {
      date: "2020-01-01",
      completed: false,
      pubs: { "venue-1": { name: "The Maypole" } },
      createdByUid: "creator",
    },
  },
  completedRecords: {
    "meet-2": {
      date: "2099-05-01",
      completed: true,
      selected: "venue-2",
    },
  },
  whereMock: vi.fn((...args) => args),
  getDocsMock: vi.fn(),
  getDocMock: vi.fn(),
  limitMock: vi.fn((value) => ["limit", value]),
  startAfterMock: vi.fn((value) => ["startAfter", value]),
}));

vi.mock("../firebase", () => ({ db: {} }));
vi.mock("firebase/firestore", () => ({
  collection: vi.fn((...args) => args),
  query: vi.fn((...args) => args),
  where: whereMock,
  orderBy: vi.fn(),
  limit: limitMock,
  startAfter: startAfterMock,
  getDocs: getDocsMock,
  getDoc: getDocMock,
  doc: vi.fn(),
}));
vi.mock("./useQueryDb", () => ({
  default: (query) => query[1][2] ? completedRecords : records,
}));

describe("Meet read compatibility", () => {
  it("exposes an overdue active Meet without changing the legacy Poll or query", () => {
    const { result } = renderHook(() => ({ legacy: usePolls(), meets: useActiveMeets() }));

    expect(whereMock).toHaveBeenCalledWith("completed", "==", false);
    expect(result.current.legacy.polls).toEqual(records);
    expect(result.current.meets.meets).toEqual({
      "meet-1": {
        date: "2020-01-01",
        votingCompleted: false,
        venues: { "venue-1": { name: "The Maypole" } },
        createdByUid: "creator",
      },
    });
    expect(result.current.meets.meets["meet-1"]).not.toHaveProperty("selectedVenueId");
    expect(result.current.meets.dates).toEqual(new Set(["2020-01-01"]));
    expect([...result.current.meets.sortedByDate()]).toEqual([
      ["meet-1", result.current.meets.meets["meet-1"]],
    ]);
  });

  it("preserves completed Meet fields, date filters and the paginated cursor", async () => {
    const cursor = { exists: () => true };
    const raw = {
      date: "2021-05-01",
      completed: true,
      selected: "venue-1",
      previous_pubs: ["venue-2"],
      restaurant: null,
      restaurant_time: "",
      completedByUid: "completer",
      pubs: { "venue-1": { name: "The Maypole" } },
    };
    getDocMock.mockResolvedValue(cursor);
    getDocsMock.mockResolvedValue({ docs: [
      { id: "meet-1", data: () => raw },
      { id: "meet-2", data: () => ({ date: "2021-04-01", completed: true }) },
    ] });

    const { result } = renderHook(() => ({ current: useCurrentMeets(), past: usePastMeets(1, "cursor-id") }));
    await waitFor(() => expect(result.current.past.lastVisibleId).toBe("meet-1"));

    const today = new Date().toISOString().slice(0, 10);
    expect(whereMock).toHaveBeenCalledWith("completed", "==", true);
    expect(whereMock).toHaveBeenCalledWith("date", ">=", today);
    expect(whereMock).toHaveBeenCalledWith("date", "<", today);
    expect(result.current.current.meets["meet-2"]).toEqual({
      date: "2099-05-01",
      votingCompleted: true,
      selectedVenueId: "venue-2",
    });
    expect(startAfterMock).toHaveBeenCalledWith(cursor);
    expect(limitMock).toHaveBeenCalledWith(2);
    expect(result.current.past.hasNextPage).toBe(true);
    expect(result.current.past.meetData.meets).toEqual({
      "meet-1": {
        date: "2021-05-01",
        votingCompleted: true,
        selectedVenueId: "venue-1",
        previousVenueIds: ["venue-2"],
        restaurantId: null,
        restaurantTime: "",
        completedByUid: "completer",
        venues: { "venue-1": { name: "The Maypole" } },
      },
    });
    expect(result.current.past.pollData.polls).toEqual({ "meet-1": raw });
  });
});