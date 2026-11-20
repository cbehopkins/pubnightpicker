// @ts-check

import { useMemo } from "react";

/** @typedef {[string, string]} PollRow */

/**
 * Hook that extracts and sorts venue rows from poll data
 * Returns array of [venueName, venueId] tuples sorted alphabetically, with "Global" always first
 * @param {{ venues?: Record<string, { name?: string } | boolean>} | null | undefined} pollData
 * @param {Record<string, { name?: string } | undefined>} [venues]
 * @returns {PollRow[]}
 */
export function usePollRows(pollData, venues) {
  return useMemo(() => {
    /** @type {PollRow[]} */
    const rows = [["Global", "any"]];

    /** @type {PollRow[]} */
    const venueRows = [];
    if (pollData && pollData.venues) {
      for (const [id, venue] of Object.entries(pollData.venues)) {
        if (id === "any") {
          continue;
        }
        const name = venues?.[id]?.name || (typeof venue === "object" && venue?.name) || "Unknown venue";
        venueRows.push([name, id]);
      }
    }

    venueRows.sort((a, b) => a[0].localeCompare(b[0]));
    rows.push(...venueRows);
    return rows;
  }, [pollData, venues]);
}
