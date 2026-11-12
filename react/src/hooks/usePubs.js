import { useSyncExternalStore } from "react";
import { lastOrdersApiURL } from "../utils/lastOrdersApiURL";

const initialState = {
  venues: {},
  isLoading: true,
  isStale: false,
  errorMessage: null,
};

let state = initialState;
let source = null;
let hasSnapshot = false;
let hasLoggedStreamError = false;
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
    isLoading: false,
    isStale: hasSnapshot,
    errorMessage: message,
  });
}

function startStream() {
  try {
    const url = lastOrdersApiURL("api/venues/stream");
    const eventSource = new EventSource(url);
    source = eventSource;
    eventSource.addEventListener("venues", (event) => {
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
      hasLoggedStreamError = false;
      publish({
        venues: Object.fromEntries(entries),
        isLoading: false,
        isStale: false,
        errorMessage: null,
      });
    });
    eventSource.onerror = () => {
      if (!hasLoggedStreamError) {
        console.error("Last Orders venue stream failed", {
          url: eventSource.url,
          readyState: eventSource.readyState,
        });
        hasLoggedStreamError = true;
      }
      if (eventSource.readyState === EventSource.CLOSED && source === eventSource) {
        eventSource.close();
        source = null;
      }
      markStreamError("Venue updates are temporarily unavailable. Try again to reconnect.");
    };
  } catch (error) {
    if (!hasLoggedStreamError) {
      console.error("Unable to create Last Orders venue stream", error);
      hasLoggedStreamError = true;
    }
    source = null;
    markStreamError("Unable to connect to the Last Orders venue stream.");
  }
}

function retryStream() {
  source?.close();
  source = null;
  hasLoggedStreamError = false;
  publish({
    ...state,
    isLoading: !hasSnapshot,
    isStale: hasSnapshot,
    errorMessage: null,
  });
  if (subscribers.size > 0) {
    startStream();
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
      hasLoggedStreamError = false;
      state = initialState;
    }
  };
}

function getSnapshot() {
  return state;
}

export function usePubsState() {
  const currentState = useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
  return { ...currentState, retry: retryStream };
}

function usePubs() {
  return usePubsState().venues;
}

export default usePubs;
