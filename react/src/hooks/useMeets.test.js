// @vitest-environment jsdom

import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useActiveMeets, useCurrentMeets, usePastMeets } from "./useMeets";

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

const eventSources = [];

class FakeEventSource {
  constructor(url) {
    this.url = url;
    this.listeners = new Map();
    this.close = vi.fn();
    eventSources.push(this);
  }

  addEventListener(name, listener) {
    this.listeners.set(name, listener);
  }

  dispatch(name, data) {
    this.listeners.get(name)?.({ data: JSON.stringify(data) });
  }
}

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

afterEach(() => {
  vi.unstubAllGlobals();
  eventSources.length = 0;
});

describe("Meet read compatibility", () => {
  it("exposes an overdue active Meet without changing the query", () => {
    const { result } = renderHook(() => ({ meets: useActiveMeets() }));

    expect(whereMock).toHaveBeenCalledWith("completed", "==", false);
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
    getDocsMock.mockResolvedValue({
      docs: [
        { id: "meet-1", data: () => raw },
        { id: "meet-2", data: () => ({ date: "2021-04-01", completed: true }) },
      ]
    });

    vi.stubGlobal("EventSource", FakeEventSource);
    const { result } = renderHook(() => ({ current: useCurrentMeets("group-a"), past: usePastMeets(1, "cursor-id") }));
    await waitFor(() => expect(result.current.past.lastVisibleId).toBe("meet-1"));
    await waitFor(() => expect(eventSources).toHaveLength(1));
    act(() => eventSources[0].dispatch("current_meets", {
      cursor: "cursor-1",
      meets: [{
        id: "meet-2",
        group_id: "group-a",
        date: "2099-05-01",
        voting_complete: true,
        venue: { id: "venue-2", status: "active", name: "The Maypole", address: "" },
        candidate_venue_ids: ["venue-2"],
        previous_venue_ids: [],
        restaurant_id: null,
        restaurant_time: null,
      }],
    }));

    const today = new Date().toISOString().slice(0, 10);
    expect(whereMock).toHaveBeenCalledWith("completed", "==", true);
    expect(whereMock).toHaveBeenCalledWith("date", "<", today);
    expect(result.current.current.meetData.meets["meet-2"]).toEqual({
      date: "2099-05-01",
      votingCompleted: true,
      selectedVenueId: "venue-2",
      venues: { "venue-2": true },
      previousVenueIds: [],
      restaurantId: null,
      restaurantTime: null,
      selectedVenueSummary: { id: "venue-2", status: "active", name: "The Maypole", address: "" },
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
    expect(result.current.past).not.toHaveProperty("pollData");
  });

  it("resets retired current-Meets state and reconnects without the old cursor", async () => {
    vi.stubGlobal("EventSource", FakeEventSource);
    const { result } = renderHook(() => useCurrentMeets("group-a"));
    await waitFor(() => expect(eventSources).toHaveLength(1));
    act(() => eventSources[0].dispatch("current_meets", {
      cursor: "old-cursor",
      meets: [{
        id: "old-meet", group_id: "group-a", date: "2026-11-20", voting_complete: true,
        venue: null, candidate_venue_ids: [], previous_venue_ids: [], restaurant_id: null, restaurant_time: null,
      }],
    }));
    expect(result.current.meetData.meets).toHaveProperty("old-meet");

    act(() => eventSources[0].dispatch("view_reset", {}));
    await waitFor(() => expect(eventSources).toHaveLength(2));
    expect(eventSources[0].close).toHaveBeenCalledOnce();
    expect(result.current.isLoading).toBe(true);
    expect(result.current.meetData.meets).toEqual({});

    act(() => eventSources[1].dispatch("current_meets", {
      cursor: "new-cursor",
      meets: [{
        id: "new-meet", group_id: "group-a", date: "2026-11-21", voting_complete: true,
        venue: null, candidate_venue_ids: [], previous_venue_ids: [], restaurant_id: null, restaurant_time: null,
      }],
    }));
    expect(result.current.meetData.meets).toHaveProperty("new-meet");
  });
});
