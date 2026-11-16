// @vitest-environment jsdom

import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import usePubs, { usePubsState } from "./usePubs";
import useVenues, { normalizeVenue as normalizeDomainVenue, useVenuesState } from "./useVenues";

vi.mock("../firebase", () => {
  throw new Error("Public venue streams must not initialise Firebase.");
});

const eventSources = [];

class FakeEventSource {
  static CLOSED = 2;

  constructor(url) {
    this.url = url;
    this.readyState = 0;
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

  it("exposes domain-named venue fields alongside the existing form on one stream", () => {
    const domain = renderHook(() => useVenues());
    const legacy = renderHook(() => usePubs());
    expect(eventSources).toHaveLength(1);

    sendSnapshot(eventSources[0], [{
      id: "venue-1", name: "Festival", website_url: "https://example.test",
      map_url: "https://maps.example.test", image_url: "https://images.example.test",
      dog_friendly: false, beer_garden: true, out_of_town: false,
      edit_tokens: { name: "edit-token" }, delete_token: "delete-token",
    }]);

    expect(domain.result.current["venue-1"]).toEqual({
      name: "Festival", venueType: "pub", website: "https://example.test",
      map: "https://maps.example.test", image: "https://images.example.test",
      dogFriendly: false, beerGarden: true, outOfTown: false,
      editTokens: { name: "edit-token" }, deleteToken: "delete-token",
    });
    expect(legacy.result.current["venue-1"]).toMatchObject({
      web_site: "https://example.test", pubImage: "https://images.example.test",
      dog_friend: false, beer_gerden: true, out_of_town: false,
      edit_tokens: { name: "edit-token" }, delete_token: "delete-token",
    });
  });

  it("preserves optional API values and domain metadata in stream and conflict snapshots", () => {
    const apiVenue = {
      id: "venue-1", name: "Festival", venue_type: "event", website_url: null,
      map_url: "", image_url: null, location_id: "cambridge", next_occurrence_date: "2027-06-12",
      visible_group_ids: ["group-1"], previous_venue_ids: ["old-venue"],
      recurrence: { frequency: "yearly", month: 6, month_day: 12 },
      edit_tokens: { venue_type: "type-token" }, delete_token: "document-token",
    };
    const { result } = renderHook(() => useVenuesState());
    sendSnapshot(eventSources[0], [apiVenue]);
    const expected = {
      name: "Festival", venueType: "event", website: null, map: "", image: null,
      locationId: "cambridge", nextOccurrenceDate: "2027-06-12",
      visibleGroupIds: ["group-1"], previousVenueIds: ["old-venue"],
      recurrence: { frequency: "yearly", month: 6, month_day: 12 },
      editTokens: { venue_type: "type-token" }, deleteToken: "document-token",
    };
    expect(result.current.venues["venue-1"]).toEqual(expected);
    expect(normalizeDomainVenue(apiVenue)).toEqual(expected);
    expect(normalizeDomainVenue({ name: "Missing ID" })).toBeNull();
  });

  it("keeps domain and legacy consumers in sync through stream errors and retry", () => {
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});
    const domain = renderHook(() => useVenuesState());
    const legacy = renderHook(() => usePubsState());
    expect(domain.result.current).toMatchObject({ venues: {}, isLoading: true, isStale: false, errorMessage: null });
    sendSnapshot(eventSources[0], [{ id: "venue-1", name: "The Maypole", dog_friendly: false }]);

    act(() => eventSources[0].dispatch("venues", { venues: [{ id: "venue-2", name: "Invalid" }], cursor: "" }));
    expect(domain.result.current).toMatchObject({
      venues: { "venue-1": { name: "The Maypole", venueType: "pub", dogFriendly: false } },
      isLoading: false, isStale: true, errorMessage: "Last Orders returned an invalid venue snapshot.",
    });
    expect(legacy.result.current).toMatchObject({
      venues: { "venue-1": { name: "The Maypole", venueType: "pub", dog_friend: false } },
      isStale: true,
    });

    act(() => domain.result.current.retry());
    expect(domain.result.current).toMatchObject({ isLoading: false, isStale: true, errorMessage: null });
    expect(eventSources).toHaveLength(2);
    sendSnapshot(eventSources[1], [{ id: "venue-2", name: "Cafe", venue_type: "restaurant" }]);
    expect(domain.result.current).toMatchObject({
      venues: { "venue-2": { name: "Cafe", venueType: "restaurant" } },
      isLoading: false, isStale: false, errorMessage: null,
    });
    expect(legacy.result.current.venues).toEqual({ "venue-2": { name: "Cafe", venueType: "restaurant" } });
    consoleError.mockRestore();
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
        edit_tokens: { name: "signed-name-baseline" },
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
        edit_tokens: { name: "signed-name-baseline" },
      },
      "venue-2": { name: "The Maypole", venueType: "pub" },
    });
  });

  it("replaces the full venue collection on each snapshot", () => {
    const { result } = renderHook(() => usePubs());
    const source = eventSources[0];
    sendSnapshot(source, [
      { id: "venue-1", name: "The Maypole", venue_type: "restaurant", edit_tokens: { name: "maypole-v1" } },
      { id: "venue-2", name: "The Crown", venue_type: "pub" },
    ]);
    sendSnapshot(source, [{
      id: "venue-2",
      name: "The Crown",
      venue_type: "event",
      edit_tokens: { name: "crown-v2" },
    }]);

    expect(result.current).toEqual({
      "venue-2": { name: "The Crown", venueType: "event", edit_tokens: { name: "crown-v2" } },
    });
  });

  it("retains the last snapshot as stale until a reconnect snapshot arrives", () => {
    const { result } = renderHook(() => usePubsState());
    const source = eventSources[0];
    expect(result.current.isLoading).toBe(true);

    sendSnapshot(source, [{
      id: "venue-1",
      name: "The Maypole",
      venue_type: "pub",
      edit_tokens: { name: "maypole-v1" },
    }]);
    expect(result.current).toMatchObject({ isLoading: false, isStale: false });

    act(() => source.interrupt());
    expect(result.current).toMatchObject({
      venues: { "venue-1": { name: "The Maypole", venueType: "pub" } },
      isStale: true,
    });

    sendSnapshot(source, [{
      id: "venue-2",
      name: "The Crown",
      venue_type: "event",
      edit_tokens: { name: "crown-v2" },
    }]);
    expect(result.current).toMatchObject({
      venues: { "venue-2": { name: "The Crown", venueType: "event", edit_tokens: { name: "crown-v2" } } },
      isStale: false,
    });
  });

  it("reports stream failures without staying in loading and allows an explicit retry", () => {
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => { });
    const { result } = renderHook(() => usePubsState());
    const failedSource = eventSources[0];
    failedSource.readyState = FakeEventSource.CLOSED;

    act(() => {
      failedSource.interrupt();
      failedSource.interrupt();
    });

    expect(result.current).toMatchObject({
      isLoading: false,
      errorMessage: "Venue updates are temporarily unavailable. Try again to reconnect.",
    });
    expect(consoleError).toHaveBeenCalledOnce();
    expect(consoleError).toHaveBeenCalledWith("Last Orders venue stream failed", {
      url: "http://localhost:8081/api/venues/stream",
      readyState: failedSource.readyState,
    });
    expect(failedSource.close).toHaveBeenCalledOnce();

    act(() => result.current.retry());

    expect(failedSource.close).toHaveBeenCalledOnce();
    expect(eventSources).toHaveLength(2);
    expect(result.current).toMatchObject({ isLoading: true, errorMessage: null });
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
    const second = renderHook(() => useVenuesState());

    expect(eventSources).toHaveLength(1);
    first.unmount();
    expect(eventSources[0].close).not.toHaveBeenCalled();

    second.unmount();
    expect(eventSources[0].close).toHaveBeenCalledOnce();
  });
});
