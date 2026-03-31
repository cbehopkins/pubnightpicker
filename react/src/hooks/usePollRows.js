import { useMemo } from "react";

/**
 * Hook that extracts and sorts pub rows from poll data
 * Returns array of [pubName, pubId] tuples sorted alphabetically, with "Global" always first
 */
export function usePollRows(pollData) {
  return useMemo(() => {
    const rows = [["Global", "any"]];

    const pubRows = [];
    if (pollData && pollData.pubs) {
      for (const [id, pub] of Object.entries(pollData.pubs)) {
        if (id === "any") {
          continue;
        }
        pubRows.push([pub.name, id]);
      }
    }

    pubRows.sort((a, b) => a[0].localeCompare(b[0]));
    rows.push(...pubRows);
    return rows;
  }, [pollData]);
}
