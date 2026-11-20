// @vitest-environment jsdom

import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useActiveMeets, useCurrentMeets, usePastMeets } from "./useMeets";

const { records, whereMock, requestLastOrdersMock } = vi.hoisted(() => ({
  records: {
    "meet-1": {
      date: "2020-01-01",
      completed: false,
      pubs: { "venue-1": { name: "The Maypole" } },
      createdByUid: "creator",
    },
  },
  whereMock: vi.fn((...args) => args),
  requestLastOrdersMock: vi.fn(),
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
}));
vi.mock("./useQueryDb", () => ({
  default: () => records,
}));
vi.mock("../utils/lastOrdersApi", () => ({ requestLastOrders: requestLastOrdersMock }));

afterEach(() => {
  vi.useRealTimers();
  requestLastOrdersMock.mockReset();
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

  it("loads a Group-scoped past-Meets page with the opaque cursor and Venue summary", async () => {
    requestLastOrdersMock.mockResolvedValue({
      meets: [{
        id: "meet-1",
        date: "2021-05-01",
        venue: {
          id: "venue-1", status: "deleted", name: "The Maypole", address: "High Street",
          website_url: "https://maypole.example", image_url: "https://maypole.example/photo.jpg",
        },
      }],
      next_cursor: "opaque-next-cursor",
    });

    vi.stubGlobal("EventSource", FakeEventSource);
    const { result } = renderHook(() => ({ current: useCurrentMeets("group-a"), past: usePastMeets("group-a", 1, "opaque-before-cursor") }));
    await waitFor(() => expect(result.current.past.nextCursor).toBe("opaque-next-cursor"));
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

    expect(requestLastOrdersMock).toHaveBeenCalledWith("api/meets/group-a/past", expect.objectContaining({
      method: "GET",
      query: { limit: "1", before: "opaque-before-cursor" },
    }));
    expect(whereMock).not.toHaveBeenCalledWith("completed", "==", true);
    expect(whereMock).not.toHaveBeenCalledWith("date", "<", expect.any(String));
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
    expect(result.current.past.hasNextPage).toBe(true);
    expect(result.current.past.meetData.meets).toEqual({
      "meet-1": {
        date: "2021-05-01",
        votingCompleted: true,
        selectedVenueId: "venue-1",
        venues: { "venue-1": true },
        selectedVenueSummary: {
          id: "venue-1", status: "deleted", name: "The Maypole", address: "High Street",
          website: "https://maypole.example", image: "https://maypole.example/photo.jpg",
        },
      },
    });
    expect(result.current.past).not.toHaveProperty("pollData");
  });

  it("keeps temporary API failures visible and retryable", async () => {
    requestLastOrdersMock.mockRejectedValue({
      code: "past_meets_unavailable",
      message: "Past Meets are temporarily unavailable.",
    });
    const { result } = renderHook(() => usePastMeets("group-a", 5, null));

    await waitFor(() => expect(result.current.errorCode).toBe("past_meets_unavailable"));

    expect(result.current.errorMessage).toBe("Past Meets are temporarily unavailable.");
    expect(result.current.meetData.meets).toEqual({});
    expect(result.current.hasNextPage).toBe(false);
  });

  it("refreshes the first page at the London date boundary across spring DST", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-03-29T00:00:05.000Z"));
    requestLastOrdersMock.mockResolvedValue({ meets: [], next_cursor: null });
    const { unmount } = renderHook(() => usePastMeets("group-a", 5, null));

    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(requestLastOrdersMock).toHaveBeenCalledTimes(1);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(23 * 60 * 60 * 1000);
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(requestLastOrdersMock).toHaveBeenCalledTimes(2);
    unmount();
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
