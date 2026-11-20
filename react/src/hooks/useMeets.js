import { useState, useEffect, useMemo, useCallback, useSyncExternalStore } from "react";
import {
  query,
  collection,
  where,
  orderBy,
  limit,
  startAfter,
  getDocs,
  getDoc,
  doc,
} from "firebase/firestore";
import { db } from "../firebase";
import useQueryDb from "./useQueryDb";
import { MeetData, getTodaysDate, millisecUntilMidnight } from "../utils/meetSorting";
import { lastOrdersApiURL } from "../utils/lastOrdersApiURL";

const currentMeetStreams = new Map();
const noCurrentGroup = {
  meetData: new MeetData({}),
  isLoading: false,
  isStale: false,
  errorMessage: "The current Group is unavailable.",
  retry: () => {},
};

function useTodaysDate() {
  const [todaysDate, setTodaysDate] = useState(getTodaysDate());

  useEffect(() => {
    // When the date changes, then the Meets we should filter by should also change
    const timeout = millisecUntilMidnight();
    const timeoutId = setTimeout(() => {
      setTodaysDate(getTodaysDate());
    }, timeout + 2000);

    return () => {
      clearTimeout(timeoutId);
    };
  }, [todaysDate]);

  return todaysDate;
}

function createCurrentMeetStream(groupId) {
  return {
    groupId,
    source: null,
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

function currentMeetData(payload) {
  if (!payload || !Array.isArray(payload.meets)) {
    throw new Error("Last Orders returned an invalid current-Meets snapshot.");
  }
  const records = {};
  for (const meet of payload.meets) {
    if (!meet || typeof meet.id !== "string" || typeof meet.date !== "string" ||
      typeof meet.voting_complete !== "boolean" || !Array.isArray(meet.candidate_venue_ids) ||
      !Array.isArray(meet.previous_venue_ids)) {
      throw new Error("Last Orders returned an invalid current-Meet record.");
    }
    const candidateIds = meet.candidate_venue_ids;
    const previousIds = meet.previous_venue_ids;
    if (!candidateIds.every((id) => typeof id === "string") || !previousIds.every((id) => typeof id === "string")) {
      throw new Error("Last Orders returned invalid current-Meet Venue references.");
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

function markCurrentMeetStreamError(stream, message, source) {
  if (source && stream.source !== source) return;
  if (!stream.hasLoggedError) {
    console.error("Current-Meets stream failed", { groupId: stream.groupId, message });
    stream.hasLoggedError = true;
  }
  publishCurrentMeetState(stream, {
    ...stream.state,
    isLoading: false,
    isStale: stream.hasSnapshot,
    errorMessage: message,
  });
}

function startCurrentMeetStream(stream) {
  try {
    const url = lastOrdersApiURL(`api/meets/${encodeURIComponent(stream.groupId)}/current/stream`);
    const source = new EventSource(url);
    stream.source = source;
    source.addEventListener("current_meets", (event) => {
      let payload;
      try {
        payload = JSON.parse(event.data);
      } catch {
        markCurrentMeetStreamError(stream, "Last Orders returned an invalid current-Meets snapshot.", source);
        return;
      }
      if (!payload || typeof payload.cursor !== "string" || payload.cursor.length === 0) {
        markCurrentMeetStreamError(stream, "Last Orders returned an invalid current-Meets snapshot.", source);
        return;
      }
      let meetData;
      try {
        meetData = currentMeetData(payload);
      } catch (error) {
        markCurrentMeetStreamError(stream, error.message, source);
        return;
      }
      if (stream.source !== source) return;
      stream.hasSnapshot = true;
      stream.hasLoggedError = false;
      publishCurrentMeetState(stream, { meetData, isLoading: false, isStale: false, errorMessage: null });
    });
    source.addEventListener("view_reset", () => {
      if (stream.source !== source) return;
      source.close();
      stream.source = null;
      stream.hasSnapshot = false;
      publishCurrentMeetState(stream, {
        meetData: new MeetData({}), isLoading: true, isStale: false, errorMessage: null,
      });
      queueMicrotask(() => {
        if (stream.subscribers.size > 0 && stream.source === null) startCurrentMeetStream(stream);
      });
    });
    source.onerror = () => {
      markCurrentMeetStreamError(stream, "Current Meets are temporarily unavailable. Try again to reconnect.", source);
      if (source.readyState === EventSource.CLOSED && stream.source === source) {
        source.close();
        stream.source = null;
      }
    };
  } catch {
    stream.source = null;
    markCurrentMeetStreamError(stream, "Unable to connect to the current-Meets stream.");
  }
}

function subscribeCurrentMeets(groupId, streamKey, subscriber) {
  let stream = currentMeetStreams.get(streamKey);
  if (!stream) {
    stream = createCurrentMeetStream(groupId);
    currentMeetStreams.set(streamKey, stream);
  }
  stream.subscribers.add(subscriber);
  if (stream.subscribers.size === 1) startCurrentMeetStream(stream);
  return () => {
    stream.subscribers.delete(subscriber);
    if (stream.subscribers.size === 0) {
      stream.source?.close();
      stream.source = null;
      currentMeetStreams.delete(streamKey);
    }
  };
}

function retryCurrentMeets(streamKey) {
  const stream = currentMeetStreams.get(streamKey);
  if (!stream) return;
  stream.source?.close();
  stream.source = null;
  stream.hasLoggedError = false;
  publishCurrentMeetState(stream, {
    ...stream.state,
    isLoading: !stream.hasSnapshot,
    isStale: stream.hasSnapshot,
    errorMessage: null,
  });
  if (stream.subscribers.size > 0) startCurrentMeetStream(stream);
}

export function useCurrentMeets(groupId, userId = "") {
  const streamKey = groupId ? `${groupId}\u0000${userId ?? ""}` : null;
  const subscribe = useCallback(
    (subscriber) => groupId ? subscribeCurrentMeets(groupId, streamKey, subscriber) : () => {},
    [groupId, streamKey],
  );
  const getSnapshot = useCallback(
    () => (streamKey ? currentMeetStreams.get(streamKey)?.state : null) ?? noCurrentGroup,
    [streamKey],
  );
  const state = useSyncExternalStore(
    subscribe,
    getSnapshot,
    () => noCurrentGroup,
  );
  return groupId ? { ...state, retry: () => retryCurrentMeets(streamKey) } : noCurrentGroup;
}

export function usePastMeets(pageSize = 5, cursorId = null) {
  const todaysDate = useTodaysDate();
  const baseQuery = useMemo(
    () =>
      query(
        collection(db, "polls"),
        where("completed", "==", true),
        where("date", "<", todaysDate),
        orderBy("date", "desc")
      ),
    [todaysDate]
  );

  const [meetRecords, setMeetRecords] = useState({});
  const [lastVisibleId, setLastVisibleId] = useState(null);
  const [hasNextPage, setHasNextPage] = useState(false);
  const [isLoading, setIsLoading] = useState(false);

  const fetchPage = useCallback(async () => {
    setIsLoading(true);
    try {
      let pageQuery = query(baseQuery, limit(pageSize + 1));

      if (cursorId) {
        const cursorSnapshot = await getDoc(doc(db, "polls", cursorId));
        if (cursorSnapshot.exists()) {
          pageQuery = query(baseQuery, startAfter(cursorSnapshot), limit(pageSize + 1));
        }
      }

      const snapshot = await getDocs(pageQuery);
      let docs = [...snapshot.docs];
      const computedHasNextPage = docs.length > pageSize;
      if (computedHasNextPage) {
        docs = docs.slice(0, pageSize);
      }

      const nextMeetRecords = docs.reduce((acc, snap) => {
        acc[snap.id] = snap.data();
        return acc;
      }, {});

      setMeetRecords(nextMeetRecords);
      setLastVisibleId(docs[docs.length - 1]?.id || null);
      setHasNextPage(computedHasNextPage);
    } finally {
      setIsLoading(false);
    }
  }, [baseQuery, pageSize, cursorId]);

  useEffect(() => {
    fetchPage();
  }, [fetchPage]);

  return {
    meetData: new MeetData(meetRecords),
    hasNextPage,
    lastVisibleId,
    isLoading,
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
