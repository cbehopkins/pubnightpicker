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
import { PollData, getTodaysDate, millisecUntilMidnight } from "../utils/pollSorting";

function useTodaysDate() {
  const [todaysDate, setTodaysDate] = useState(getTodaysDate());

  useEffect(() => {
    // When the date changes, then the events we should filter by should also change
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

export function useFutureCompletePolls() {
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
  const polls = useQueryDb(q);
  return new PollData(polls);
}

export function usePastCompletePolls(pubCount = 5, cursorId = null) {
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

  const [polls, setPolls] = useState({});
  const [lastVisibleId, setLastVisibleId] = useState(null);
  const [hasNextPage, setHasNextPage] = useState(false);
  const [isLoading, setIsLoading] = useState(false);

  const fetchPage = useCallback(async () => {
    setIsLoading(true);
    try {
      let pageQuery = query(baseQuery, limit(pubCount + 1));

      if (cursorId) {
        const cursorSnapshot = await getDoc(doc(db, "polls", cursorId));
        if (cursorSnapshot.exists()) {
          pageQuery = query(baseQuery, startAfter(cursorSnapshot), limit(pubCount + 1));
        }
      }

      const snapshot = await getDocs(pageQuery);
      let docs = [...snapshot.docs];
      const computedHasNextPage = docs.length > pubCount;
      if (computedHasNextPage) {
        docs = docs.slice(0, pubCount);
      }

      const nextPolls = docs.reduce((acc, snap) => {
        acc[snap.id] = snap.data();
        return acc;
      }, {});

      setPolls(nextPolls);
      setLastVisibleId(docs[docs.length - 1]?.id || null);
      setHasNextPage(computedHasNextPage);
    } finally {
      setIsLoading(false);
    }
  }, [baseQuery, pubCount, cursorId]);

  useEffect(() => {
    fetchPage();
  }, [fetchPage]);

  return {
    pollData: new PollData(polls),
    hasNextPage,
    lastVisibleId,
    isLoading,
  };
}

function usePolls() {
  const q = useMemo(
    () => query(collection(db, "polls"), where("completed", "==", false)),
    []
  );
  const polls = useQueryDb(q);
  return new PollData(polls);
}

export default usePolls;
