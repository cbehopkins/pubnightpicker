// @ts-check

import { useMemo } from "react";

/** @typedef {[string, string]} PollRow */

/**
 * Hook that extracts and sorts pub rows from poll data
 * Returns array of [pubName, pubId] tuples sorted alphabetically, with "Global" always first
 * @param {{ pubs?: Record<string, { name?: string }>} | null | undefined} pollData
 * @returns {PollRow[]}
 */
export function usePollRows(pollData) {
  return useMemo(() => {
    /** @type {PollRow[]} */
    const rows = [["Global", "any"]];

    /** @type {PollRow[]} */
    const pubRows = [];
    if (pollData && pollData.pubs) {
      for (const [id, pub] of Object.entries(pollData.pubs)) {
        if (id === "any") {
          continue;
        }
        pubRows.push([pub?.name || "Unknown venue", id]);
      }
    }

    pubRows.sort((a, b) => a[0].localeCompare(b[0]));
    rows.push(...pubRows);
    return rows;
  }, [pollData]);
}
