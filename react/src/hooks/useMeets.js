import { useState, useEffect, useMemo, useCallback } from "react";
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

export function useCurrentMeets() {
  // FIXME add limit to this too - needs a UI update though to support
  const todaysDate = useTodaysDate();
  const q = useMemo(
    () =>
      query(
        collection(db, "polls"),
        where("completed", "==", true),
        where("date", ">=", todaysDate)
      ),
    [todaysDate]
  );
  const meetRecords = useQueryDb(q);
  return new MeetData(meetRecords);
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
