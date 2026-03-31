/**
 * Utility functions for poll data manipulation and sorting
 * Extracted from usePolls.js hook to enable reusability outside React context
 */

/**
 * PollData class that wraps poll objects and provides useful transformations
 * Methods: sortedByDate (generator), dates (getter)
 */
export class PollData {
  constructor(polls) {
    this.polls = polls;
  }

  get dates() {
    const currentPollDates = new Set();
    for (const value of Object.values(this.polls)) {
      currentPollDates.add(value.date);
    }
    return currentPollDates;
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
      bob.reverse();
    }

    // Don't let our internal hackery escape, so yield so as to not
    // needlessly create yet another temp object
    for (var [, id, poll] of bob) {
      yield [id, poll];
    }
  }
}

/**
 * Gets today's date in ISO format (YYYY-MM-DD)
 * @returns {string} Today's date
 */
export function getTodaysDate() {
  const timestamp = new Date();
  return timestamp.toISOString().slice(0, 10);
}

/**
 * Calculates milliseconds until midnight (00:00 of next day)
 * Used for automatic date updates at midnight
 * @returns {number} Milliseconds until midnight
 */
export function millisecUntilMidnight() {
  var midnight = new Date();
  midnight.setHours(24);
  midnight.setMinutes(0);
  midnight.setSeconds(0);
  midnight.setMilliseconds(0);
  return midnight.getTime() - new Date().getTime();
}
