// @ts-check

import { useCallback, useMemo } from "react";
import { runAttendanceAction } from "../utils/asyncErrorHandler";

/** @typedef {"canCome" | "cannotCome"} AttendanceStatus */

/**
 * @typedef {Object} BallotActionsResult
 * @property {string[]} pollVenueIds
 * @property {() => Promise<void>} setAllAttendanceToCanCome
 * @property {() => Promise<void>} setAllAttendanceToCannotCome
 */

/**
 * Hook that manages batch ballot actions (setting attendance for all venues at once)
 * Returns handlers for setting all venues to "can come" or "cannot come"
 * @param {{ venues?: Record<string, unknown> } | null | undefined} pollData
 * @param {string | null | undefined} currUserId
 * @param {(venueIds: string[] | null | undefined, userId: string, status: AttendanceStatus) => Promise<void>} setGlobalAttendanceStatus
 * @returns {BallotActionsResult}
 */
export function useBallotActions(pollData, currUserId, setGlobalAttendanceStatus) {
  // Get all non-global venue IDs
  const pollVenueIds = useMemo(() => {
    return Object.keys(pollData?.venues || {}).filter((venueId) => venueId !== "any");
  }, [pollData?.venues]);

  // Handler to set all venues to "can come"
  const setAllAttendanceToCanCome = useCallback(async () => {
    if (!currUserId || pollVenueIds.length === 0) {
      return;
    }

    await runAttendanceAction(() =>
      setGlobalAttendanceStatus(pollVenueIds, currUserId, "canCome")
    );
  }, [currUserId, pollVenueIds, setGlobalAttendanceStatus]);

  // Handler to set all venues to "cannot come"
  const setAllAttendanceToCannotCome = useCallback(async () => {
    if (!currUserId || pollVenueIds.length === 0) {
      return;
    }

    await runAttendanceAction(() =>
      setGlobalAttendanceStatus(pollVenueIds, currUserId, "cannotCome")
    );
  }, [currUserId, pollVenueIds, setGlobalAttendanceStatus]);

  return {
    pollVenueIds,
    setAllAttendanceToCanCome,
    setAllAttendanceToCannotCome,
  };
}
