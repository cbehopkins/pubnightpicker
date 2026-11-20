import { useState, useEffect, useMemo, useCallback, useSyncExternalStore } from "react";
import {
  collection,
  query,
  where,
} from "firebase/firestore";
import { db } from "../firebase";
import useQueryDb from "./useQueryDb";
import { MeetData } from "../utils/meetSorting";
import { LastOrdersApiError, requestLastOrders, streamLastOrders } from "../utils/lastOrdersApi";

const meetStreams = {
  current: new Map(),
  open: new Map(),
};
const noCurrentGroup = {
  meetData: new MeetData({}),
  isLoading: false,
  isStale: false,
  errorMessage: "The current Group is unavailable.",
  retry: () => { },
};

const londonDateFormatter = new Intl.DateTimeFormat("en-CA", {
  timeZone: "Europe/London",
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
});

function getLondonDate() {
  const parts = Object.fromEntries(londonDateFormatter.formatToParts(new Date()).map(({ type, value }) => [type, value]));
  return `${parts.year}-${parts.month}-${parts.day}`;
}

function useLondonDate() {
  const [date, setDate] = useState(getLondonDate);
  useEffect(() => {
    const intervalID = setInterval(() => {
      const currentDate = getLondonDate();
      setDate((previousDate) => currentDate === previousDate ? previousDate : currentDate);
    }, 60_000);
    return () => clearInterval(intervalID);
  }, []);
  return date;
}

function createMeetStream(kind, groupId) {
  return {
    kind,
    groupId,
    controller: null,
    subscribers: new Set(),
    hasSnapshot: false,
    hasLoggedError: false,
    state: {
      meetData: new MeetData({}),
      isLoading: true,
      isStale: false,
      errorMessage: null,
    },
  };
}

function publishCurrentMeetState(stream, state) {
  stream.state = state;
  stream.subscribers.forEach((subscriber) => subscriber());
}

function meetCollectionData(payload, kind) {
  if (!payload || !Array.isArray(payload.meets)) {
    throw new Error(`Last Orders returned an invalid ${meetCollectionName(kind)} snapshot.`);
  }
  const records = {};
  for (const meet of payload.meets) {
    if (!meet || typeof meet.id !== "string" || typeof meet.date !== "string" ||
      typeof meet.voting_complete !== "boolean" || meet.voting_complete !== (kind === "current") ||
      !Array.isArray(meet.candidate_venue_ids) ||
      !Array.isArray(meet.previous_venue_ids)) {
      throw new Error(`Last Orders returned an invalid ${meetCollectionName(kind)} record.`);
    }
    const candidateIds = meet.candidate_venue_ids;
    const previousIds = meet.previous_venue_ids;
    if (!candidateIds.every((id) => typeof id === "string") || !previousIds.every((id) => typeof id === "string")) {
      throw new Error(`Last Orders returned invalid ${meetCollectionName(kind)} Venue references.`);
    }
    const selectedVenueSummary = meet.venue;
    if (selectedVenueSummary !== null && (!selectedVenueSummary || typeof selectedVenueSummary.id !== "string" ||
      !["active", "deleted", "missing"].includes(selectedVenueSummary.status))) {
      throw new Error("Last Orders returned an invalid selected Venue summary.");
    }
    records[meet.id] = {
      date: meet.date,
      completed: meet.voting_complete,
      selected: selectedVenueSummary?.id ?? "",
      pubs: Object.fromEntries(candidateIds.map((id) => [id, true])),
      previous_pubs: previousIds,
      restaurant: meet.restaurant_id,
      restaurant_time: meet.restaurant_time,
      selectedVenueSummary,
    };
  }
  return new MeetData(records);
}

function meetCollectionName(kind) {
  return kind === "open" ? "open-Meets" : "current-Meets";
}

function markMeetStreamError(stream, message, controller) {
  if (controller && stream.controller !== controller) return;
  if (!stream.hasLoggedError) {
    console.error(`${meetCollectionName(stream.kind)} stream failed`, { groupId: stream.groupId, message });
    stream.hasLoggedError = true;
  }
  publishCurrentMeetState(stream, {
    ...stream.state,
    isLoading: false,
    isStale: stream.hasSnapshot,
    errorMessage: message,
  });
}

function startMeetStream(stream) {
  const collectionPath = stream.kind === "open" ? "open" : "current";
  const eventName = stream.kind === "open" ? "open_meets" : "current_meets";
  const collectionName = stream.kind === "open" ? "Open Meets" : "Current Meets";
  const controller = new AbortController();
  stream.controller = controller;
  try {
    streamLastOrders(`api/meets/${encodeURIComponent(stream.groupId)}/${collectionPath}/stream`, {
      signal: controller.signal,
      onopen: async (response) => {
        const contentType = response.headers.get("content-type")?.split(";")[0].trim().toLowerCase();
        if (response.ok && contentType === "text/event-stream") return;
        const code = response.status === 401 ? "unauthenticated" :
          response.status === 403 ? "forbidden" : response.status === 404 ? "not_found" : "stream_unavailable";
        throw new LastOrdersApiError(code, `${collectionName} are temporarily unavailable.`, { status: response.status });
      },
      onmessage: (event) => {
        if (controller.signal.aborted || stream.controller !== controller) return;
        if (event.event === "view_reset") {
          stream.controller = null;
          controller.abort();
          stream.hasSnapshot = false;
          publishCurrentMeetState(stream, {
            meetData: new MeetData({}), isLoading: true, isStale: false, errorMessage: null,
          });
          queueMicrotask(() => {
            if (stream.subscribers.size > 0 && stream.controller === null) startMeetStream(stream);
          });
          return;
        }
        if (event.event !== eventName) return;
        let payload;
        try {
          payload = JSON.parse(event.data);
        } catch {
          markMeetStreamError(stream, `Last Orders returned an invalid ${meetCollectionName(stream.kind)} snapshot.`, controller);
          return;
        }
        if (!payload || typeof payload.cursor !== "string" || payload.cursor.length === 0) {
          markMeetStreamError(stream, `Last Orders returned an invalid ${meetCollectionName(stream.kind)} snapshot.`, controller);
          return;
        }
        let meetData;
        try {
          meetData = meetCollectionData(payload, stream.kind);
        } catch (error) {
          markMeetStreamError(stream, error.message, controller);
          return;
        }
        stream.hasSnapshot = true;
        stream.hasLoggedError = false;
        publishCurrentMeetState(stream, { meetData, isLoading: false, isStale: false, errorMessage: null });
      },
      onclose: () => {
        if (!controller.signal.aborted) throw new Error(`${collectionName} stream closed unexpectedly.`);
      },
      onerror: (error) => {
        if (controller.signal.aborted || stream.controller !== controller) return;
        markMeetStreamError(stream, error?.message || `${collectionName} are temporarily unavailable.`, controller);
        if (error instanceof LastOrdersApiError && ["signed_out", "unauthenticated", "forbidden", "not_found"].includes(error.code)) {
          throw error;
        }
        return 5000;
      },
    }).catch((error) => {
      if (controller.signal.aborted || stream.controller !== controller) return;
      stream.controller = null;
      markMeetStreamError(stream, error?.message || `Unable to connect to the ${meetCollectionName(stream.kind)} stream.`);
    });
  } catch (error) {
    if (stream.controller === controller) stream.controller = null;
    markMeetStreamError(stream, error?.message || `Unable to connect to the ${meetCollectionName(stream.kind)} stream.`);
  }
}

function subscribeMeetCollection(kind, groupId, streamKey, subscriber) {
  const collectionStreams = meetStreams[kind];
  let stream = collectionStreams.get(streamKey);
  if (!stream) {
    stream = createMeetStream(kind, groupId);
    collectionStreams.set(streamKey, stream);
  }
  stream.subscribers.add(subscriber);
  if (stream.subscribers.size === 1) startMeetStream(stream);
  return () => {
    stream.subscribers.delete(subscriber);
    if (stream.subscribers.size === 0) {
      stream.controller?.abort();
      stream.controller = null;
      collectionStreams.delete(streamKey);
    }
  };
}

function retryMeetCollection(kind, streamKey) {
  const stream = meetStreams[kind].get(streamKey);
  if (!stream) return;
  stream.controller?.abort();
  stream.controller = null;
  stream.hasLoggedError = false;
  publishCurrentMeetState(stream, {
    ...stream.state,
    isLoading: !stream.hasSnapshot,
    isStale: stream.hasSnapshot,
    errorMessage: null,
  });
  if (stream.subscribers.size > 0) startMeetStream(stream);
}

function useMeetCollection(kind, groupId, userId = "") {
  const streamKey = groupId ? `${groupId}\u0000${userId ?? ""}` : null;
  const subscribe = useCallback(
    (subscriber) => groupId ? subscribeMeetCollection(kind, groupId, streamKey, subscriber) : () => { },
    [kind, groupId, streamKey],
  );
  const getSnapshot = useCallback(
    () => (streamKey ? meetStreams[kind].get(streamKey)?.state : null) ?? noCurrentGroup,
    [kind, streamKey],
  );
  const state = useSyncExternalStore(
    subscribe,
    getSnapshot,
    () => noCurrentGroup,
  );
  return groupId ? { ...state, retry: () => retryMeetCollection(kind, streamKey) } : noCurrentGroup;
}

export function useCurrentMeets(groupId, userId = "") {
  return useMeetCollection("current", groupId, userId);
}

export function useOpenMeets(groupId, userId = "") {
  return useMeetCollection("open", groupId, userId);
}

function pastMeetData(payload) {
  if (!payload || !Array.isArray(payload.meets) ||
    (payload.next_cursor !== null && typeof payload.next_cursor !== "string")) {
    throw new Error("Last Orders returned an invalid past-Meets page.");
  }
  const records = {};
  for (const meet of payload.meets) {
    if (!meet || typeof meet.id !== "string" || typeof meet.date !== "string") {
      throw new Error("Last Orders returned an invalid past-Meet record.");
    }
    const venue = meet.venue;
    if (venue !== null && (!venue || typeof venue.id !== "string" ||
      !["active", "deleted", "missing"].includes(venue.status))) {
      throw new Error("Last Orders returned an invalid past-Meet Venue summary.");
    }
    const selectedVenueSummary = venue === null ? null : {
      id: venue.id,
      status: venue.status,
      name: typeof venue.name === "string" ? venue.name : "",
      address: typeof venue.address === "string" ? venue.address : "",
      website: typeof venue.website_url === "string" ? venue.website_url : "",
      image: typeof venue.image_url === "string" ? venue.image_url : "",
    };
    records[meet.id] = {
      date: meet.date,
      completed: true,
      selected: selectedVenueSummary?.id ?? "",
      pubs: selectedVenueSummary ? { [selectedVenueSummary.id]: true } : {},
      selectedVenueSummary,
    };
  }
  return {
    meetData: new MeetData(records),
    nextCursor: payload.next_cursor,
  };
}

export function usePastMeets(groupId, pageSize = 5, cursor = null) {
  const londonDate = useLondonDate();
  const [retryCount, setRetryCount] = useState(0);
  const requestKey = JSON.stringify([groupId, pageSize, cursor, londonDate, retryCount]);
  const [state, setState] = useState({
    requestKey: null,
    meetData: new MeetData({}),
    nextCursor: null,
    hasNextPage: false,
    isLoading: Boolean(groupId),
    errorMessage: groupId ? null : "The current Group is unavailable.",
    errorCode: null,
  });
  useEffect(() => {
    if (!groupId) {
      setState({
        requestKey,
        meetData: new MeetData({}), nextCursor: null, hasNextPage: false,
        isLoading: false, errorMessage: "The current Group is unavailable.", errorCode: null,
      });
      return undefined;
    }
    const controller = new AbortController();
    setState({ requestKey, meetData: new MeetData({}), nextCursor: null, hasNextPage: false, isLoading: true, errorMessage: null, errorCode: null });
    requestLastOrders(`api/meets/${encodeURIComponent(groupId)}/past`, {
      method: "GET",
      query: { limit: String(pageSize), ...(cursor ? { before: cursor } : {}) },
      signal: controller.signal,
    }).then((payload) => {
      try {
        const page = pastMeetData(payload);
        setState({
          requestKey,
          ...page,
          hasNextPage: page.nextCursor !== null,
          isLoading: false,
          errorMessage: null,
          errorCode: null,
        });
      } catch (error) {
        setState({ requestKey, meetData: new MeetData({}), nextCursor: null, hasNextPage: false, isLoading: false, errorMessage: error.message, errorCode: "invalid_response" });
      }
    }).catch((error) => {
      if (controller.signal.aborted) return;
      setState({
        requestKey,
        meetData: new MeetData({}), nextCursor: null, hasNextPage: false, isLoading: false,
        errorMessage: error?.message || "Past Meets are temporarily unavailable.",
        errorCode: error?.code || "request_error",
      });
    });
    return () => controller.abort();
  }, [groupId, pageSize, cursor, retryCount, londonDate, requestKey]);

  const requestIsCurrent = state.requestKey === requestKey;
  return {
    ...state,
    meetData: requestIsCurrent ? state.meetData : new MeetData({}),
    nextCursor: requestIsCurrent ? state.nextCursor : null,
    hasNextPage: requestIsCurrent && state.hasNextPage,
    isLoading: !requestIsCurrent || state.isLoading,
    errorMessage: requestIsCurrent ? state.errorMessage : null,
    errorCode: requestIsCurrent ? state.errorCode : null,
    retry: () => setRetryCount((count) => count + 1),
  };
}

export function useActiveMeets() {
  const q = useMemo(
    () => query(collection(db, "polls"), where("completed", "==", false)),
    []
  );
  const meetRecords = useQueryDb(q);
  return new MeetData(meetRecords);
}
