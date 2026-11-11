import { useSyncExternalStore } from "react";
import { lastOrdersApiURL } from "../utils/lastOrdersApi";

const initialState = {
  venues: {},
  isLoading: true,
  isStale: false,
  errorMessage: null,
};

let state = initialState;
let source = null;
let hasSnapshot = false;
const subscribers = new Set();

function publish(nextState) {
  state = nextState;
  subscribers.forEach((subscriber) => subscriber());
}

function normalizeVenue(venue) {
  if (!venue || typeof venue !== "object" || typeof venue.id !== "string" || !venue.id || typeof venue.name !== "string") {
    return null;
  }

  const {
    venue_type: venueType,
    website_url: website,
    map_url: map,
    image_url: image,
    dog_friendly: dogFriendly,
    beer_garden: beerGarden,
    ...remainingFields
  } = venue;
  const normalized = {
    ...remainingFields,
    venueType: venueType || "pub",
  };
  delete normalized.id;

  if (website != null) normalized.web_site = website;
  if (map != null) normalized.map = map;
  if (image != null) normalized.pubImage = image;
  if (dogFriendly != null) normalized.dog_friend = dogFriendly;
  if (beerGarden != null) normalized.beer_gerden = beerGarden;
  return normalized;
}

function markStreamError(message) {
  publish({
    ...state,
    isLoading: !hasSnapshot,
    isStale: hasSnapshot,
    errorMessage: message,
  });
}

function startStream() {
  try {
    source = new EventSource(lastOrdersApiURL("api/venues/stream"));
    source.addEventListener("venues", (event) => {
      let payload;
      try {
        payload = JSON.parse(event.data);
      } catch {
        markStreamError("Last Orders returned an invalid venue snapshot.");
        return;
      }

      if (!payload || !Array.isArray(payload.venues) || typeof payload.cursor !== "string" || !payload.cursor) {
        markStreamError("Last Orders returned an invalid venue snapshot.");
        return;
      }

      const entries = [];
      const venueIds = new Set();
      for (const venue of payload.venues) {
        const normalized = normalizeVenue(venue);
        if (!normalized || venueIds.has(venue.id)) {
          markStreamError("Last Orders returned an invalid venue snapshot.");
          return;
        }
        venueIds.add(venue.id);
        entries.push([venue.id, normalized]);
      }

      hasSnapshot = true;
      publish({
        venues: Object.fromEntries(entries),
        isLoading: false,
        isStale: false,
        errorMessage: null,
      });
    });
    source.onerror = () => markStreamError("Venue updates are temporarily unavailable.");
  } catch {
    source = null;
    markStreamError("Unable to connect to the Last Orders venue stream.");
  }
}

function subscribe(subscriber) {
  subscribers.add(subscriber);
  if (subscribers.size === 1) {
    startStream();
  }
  return () => {
    subscribers.delete(subscriber);
    if (subscribers.size === 0) {
      source?.close();
      source = null;
      hasSnapshot = false;
      state = initialState;
    }
  };
}

function getSnapshot() {
  return state;
}

export function usePubsState() {
  return useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
}

function usePubs() {
  return usePubsState().venues;
}

export default usePubs;
