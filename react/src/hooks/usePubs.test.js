// @vitest-environment jsdom

import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import usePubs, { usePubsState } from "./usePubs";

const eventSources = [];

class FakeEventSource {
  constructor(url) {
    this.url = url;
    this.listeners = new Map();
    this.close = vi.fn();
    eventSources.push(this);
  }

  addEventListener(type, listener) {
    this.listeners.set(type, listener);
  }

  removeEventListener(type) {
    this.listeners.delete(type);
  }

  dispatch(type, data) {
    this.listeners.get(type)?.({ data: JSON.stringify(data) });
  }

  interrupt() {
    this.onerror?.(new Event("error"));
  }
}

function sendSnapshot(source, venues) {
  act(() => source.dispatch("venues", { venues, cursor: "opaque-cursor" }));
}

describe("usePubs", () => {
  beforeEach(() => {
    eventSources.length = 0;
    vi.stubEnv("VITE_LAST_ORDERS_API_BASE_URL", "");
    vi.stubGlobal("EventSource", FakeEventSource);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.unstubAllEnvs();
  });

  it("maps API snapshots to the existing venue shape and defaults missing types to pub", () => {
    const { result } = renderHook(() => usePubs());
    const source = eventSources[0];

    expect(source.url).toBe("http://localhost:8081/api/venues/stream");
    expect(source.withCredentials).not.toBe(true);
    sendSnapshot(source, [
      {
        id: "venue-1",
        name: "Cambridge Beer Festival",
        website_url: "https://festival.example.test",
        map_url: "https://maps.example.test/festival",
        image_url: "https://images.example.test/festival.jpg",
        dog_friendly: true,
        beer_garden: false,
        recurrence: { frequency: "yearly", month: 6, month_day: 12 },
      },
      { id: "venue-2", name: "The Maypole", venue_type: "pub" },
    ]);

    expect(result.current).toEqual({
      "venue-1": {
        name: "Cambridge Beer Festival",
        venueType: "pub",
        web_site: "https://festival.example.test",
        map: "https://maps.example.test/festival",
        pubImage: "https://images.example.test/festival.jpg",
        dog_friend: true,
        beer_gerden: false,
        recurrence: { frequency: "yearly", month: 6, month_day: 12 },
      },
      "venue-2": { name: "The Maypole", venueType: "pub" },
    });
  });

  it("replaces the full venue collection on each snapshot", () => {
    const { result } = renderHook(() => usePubs());
    const source = eventSources[0];
    sendSnapshot(source, [
      { id: "venue-1", name: "The Maypole", venue_type: "restaurant" },
      { id: "venue-2", name: "The Crown", venue_type: "pub" },
    ]);
    sendSnapshot(source, [{ id: "venue-2", name: "The Crown", venue_type: "event" }]);

    expect(result.current).toEqual({
      "venue-2": { name: "The Crown", venueType: "event" },
    });
  });

  it("retains the last snapshot as stale until a reconnect snapshot arrives", () => {
    const { result } = renderHook(() => usePubsState());
    const source = eventSources[0];
    expect(result.current.isLoading).toBe(true);

    sendSnapshot(source, [{ id: "venue-1", name: "The Maypole", venue_type: "pub" }]);
    expect(result.current).toMatchObject({ isLoading: false, isStale: false });

    act(() => source.interrupt());
    expect(result.current).toMatchObject({
      venues: { "venue-1": { name: "The Maypole", venueType: "pub" } },
      isStale: true,
    });

    sendSnapshot(source, [{ id: "venue-2", name: "The Crown", venue_type: "event" }]);
    expect(result.current).toMatchObject({
      venues: { "venue-2": { name: "The Crown", venueType: "event" } },
      isStale: false,
    });
  });

  it("ignores malformed snapshots without clearing loaded venues", () => {
    const { result } = renderHook(() => usePubs());
    const source = eventSources[0];
    sendSnapshot(source, [{ id: "venue-1", name: "The Maypole", venue_type: "pub" }]);

    act(() => source.dispatch("venues", { venues: [{ name: "Missing ID" }] }));

    expect(result.current).toEqual({
      "venue-1": { name: "The Maypole", venueType: "pub" },
    });
  });

  it("shares one public stream and closes it after the last consumer unmounts", () => {
    const first = renderHook(() => usePubs());
    const second = renderHook(() => usePubsState());

    expect(eventSources).toHaveLength(1);
    first.unmount();
    expect(eventSources[0].close).not.toHaveBeenCalled();

    second.unmount();
    expect(eventSources[0].close).toHaveBeenCalledOnce();
  });
});
