import { useState, useEffect, useMemo } from "react";
import { query, collection, where, orderBy } from "firebase/firestore";
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
  return todaysDate

}
export function useFutureCompletePolls() {
  // FIXME add limit to this too -  needs a ui update though to support
  const todaysDate = useTodaysDate();
  const q = useMemo(() => query(
    collection(db, "polls"),
    where("completed", "==", true),
    where("date", ">=", todaysDate)
  ), [todaysDate]);
  const polls = useQueryDb(q);
  return new PollData(polls);
}
export function usePastCompletePolls() {
  const todaysDate = useTodaysDate();
  const q = useMemo(() => query(
    collection(db, "polls"),
    where("completed", "==", true),
    where("date", "<", todaysDate),
    orderBy("date", "desc")
  ), [todaysDate]);
  const polls = useQueryDb(q);
  return new PollData(polls);
}

function usePolls() {
  const q = useMemo(() => query(collection(db, "polls"), where("completed", "==", false)), []);
  const polls = useQueryDb(q);
  return new PollData(polls);
}
export default usePolls;
