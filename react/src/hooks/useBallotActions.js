// @ts-check

import { useCallback, useMemo } from "react";
import { runAttendanceAction } from "../utils/asyncErrorHandler";

/** @typedef {"canCome" | "cannotCome"} AttendanceStatus */

/**
 * @typedef {Object} BallotActionsResult
 * @property {string[]} pollPubIds
 * @property {() => Promise<void>} setAllAttendanceToCanCome
 * @property {() => Promise<void>} setAllAttendanceToCannotCome
 */

/**
 * Hook that manages batch ballot actions (setting attendance for all pubs at once)
 * Returns handlers for setting all pubs to "can come" or "cannot come"
 * @param {{ pubs?: Record<string, unknown> } | null | undefined} pollData
 * @param {string | null | undefined} currUserId
 * @param {(pubIds: string[] | null | undefined, userId: string, status: AttendanceStatus) => Promise<void>} setGlobalAttendanceStatus
 * @returns {BallotActionsResult}
 */
export function useBallotActions(pollData, currUserId, setGlobalAttendanceStatus) {
  // Get all non-global pub IDs
  const pollPubIds = useMemo(() => {
    return Object.keys(pollData?.pubs || {}).filter((pubId) => pubId !== "any");
  }, [pollData?.pubs]);

  // Handler to set all pubs to "can come"
  const setAllAttendanceToCanCome = useCallback(async () => {
    if (!currUserId || pollPubIds.length === 0) {
      return;
    }

    await runAttendanceAction(() =>
      setGlobalAttendanceStatus(pollPubIds, currUserId, "canCome")
    );
  }, [currUserId, pollPubIds, setGlobalAttendanceStatus]);

  // Handler to set all pubs to "cannot come"
  const setAllAttendanceToCannotCome = useCallback(async () => {
    if (!currUserId || pollPubIds.length === 0) {
      return;
    }

    await runAttendanceAction(() =>
      setGlobalAttendanceStatus(pollPubIds, currUserId, "cannotCome")
    );
  }, [currUserId, pollPubIds, setGlobalAttendanceStatus]);

  return {
    pollPubIds,
    setAllAttendanceToCanCome,
    setAllAttendanceToCannotCome,
  };
}
