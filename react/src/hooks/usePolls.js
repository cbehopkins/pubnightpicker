import { useState, useEffect, useMemo } from "react";
import { query, collection, where, orderBy, limit } from "firebase/firestore";
import { db } from "../firebase";
import useQueryDb from "./useQueryDb";
class PollData {
  constructor(polls) {
    this.polls = polls
  }
  get dates() {
    const currentPollDates = new Set();
    for (const value of Object.values(this.polls)) {
      currentPollDates.add(value.date);
    }
    return currentPollDates
  }

  // Can't have generators as properties.
  // Because abstraction and consistency is a mug's game I guess?
  *sortedByDate(reverse = false) {
    // Need a temp object in order to sort
    const bob = Object.entries(this.polls)
      .map(([id, poll]) => {
        const sortBy = poll.date;
        return [sortBy, id, poll];
      })
      .sort();
    if (reverse) {
      bob.reverse()
    }

    // Don't let our internal hackery escape, so yield so as to not
    // needlessly create yet another temp object
    for (var [, id, poll] of bob) {
      yield [id, poll]
    }
  }
}

function getTodaysDate() {
  const timestamp = new Date();
  return timestamp.toISOString().slice(0, 10);
}

function millisecUntilMidnight() {
  var midnight = new Date();
  midnight.setHours(24);
  midnight.setMinutes(0);
  midnight.setSeconds(0);
  midnight.setMilliseconds(0);
  return midnight.getTime() - new Date().getTime();
}
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
export function usePastCompletePolls(pubCount) {
  const todaysDate = useTodaysDate();
  const q = useMemo(() => query(
    collection(db, "polls"),
    where("completed", "==", true),
    where("date", "<", todaysDate),
    orderBy("date", "desc"),
    limit(pubCount)
  ), [todaysDate, pubCount]);
  const polls = useQueryDb(q);
  return new PollData(polls);
}

function usePolls() {
  const q = useMemo(() => query(collection(db, "polls"), where("completed", "==", false)), []);
  const polls = useQueryDb(q);
  return new PollData(polls);
}
export default usePolls;
