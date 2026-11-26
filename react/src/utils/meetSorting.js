export class MeetData {
  constructor(meetRecords) {
    this.meets = Object.fromEntries(Object.entries(meetRecords).map(([id, record]) => {
      const { pubs, selected, previous_pubs, restaurant, restaurant_time, ...meet } = record;
      delete meet.completed;
      if (selected === "any") console.error(`Meet ${id} has unexpected selected Venue ID "any".`);
      meet.votingCompleted = typeof selected === "string" && selected !== "";
      if (Object.hasOwn(record, "pubs")) meet.venues = pubs;
      if (Object.hasOwn(record, "selected")) meet.selectedVenueId = selected;
      if (Object.hasOwn(record, "previous_pubs")) meet.previousVenueIds = previous_pubs;
      if (Object.hasOwn(record, "restaurant")) meet.restaurantId = restaurant;
      if (Object.hasOwn(record, "restaurant_time")) meet.restaurantTime = restaurant_time;
      return [id, meet];
    }));
  }

  get dates() {
    return new Set(Object.values(this.meets).map((meet) => meet.date));
  }

  *sortedByDate(reverse = false) {
    const sorted = Object.entries(this.meets)
      .map(([id, meet]) => [meet.date, id, meet])
      .sort();
    if (reverse) sorted.reverse();
    for (const [, id, meet] of sorted) {
      yield [id, meet];
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
