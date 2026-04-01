// @ts-check

import { useCallback } from "react";
import { deletePubFromPoll } from "../dbtools/polls";
import { getUserFacingErrorMessage } from "../permissions";
import { notifyError } from "../utils/notify";
import { runAttendanceAction } from "../utils/asyncErrorHandler";
import { getEffectiveAttendanceState } from "../utils/attendanceState";

/** @typedef {"canCome" | "cannotCome"} AttendanceStatus */
/** @typedef {Record<string, string[]>} VotesMap */
/** @typedef {Record<string, { canCome?: string[], cannotCome?: string[] } | undefined>} AttendanceMap */

/**
 * @typedef {Object} VotableRowState
 * @property {number} voteCount
 * @property {boolean} votedFor
 * @property {boolean} userCanCome
 * @property {boolean} userCannotCome
 * @property {string[]} canCome
 * @property {string[]} cannotCome
 * @property {boolean} canVote
 * @property {boolean} allowAttendanceControls
 * @property {boolean} allowGlobalAttendanceControls
 * @property {boolean} hasAttendanceData
 * @property {() => Promise<void>} voteHandler
 * @property {(status: AttendanceStatus) => Promise<void>} setAttendanceStatusHandler
 * @property {() => Promise<void>} clearAttendanceHandler
 * @property {() => Promise<void>} deleteHandler
 */

/**
 * Hook that encapsulates all the logic for a single votable row in a poll
 * Manages vote state, attendance state, and associated handlers
 * @param {string} pubId
 * @param {string | null | undefined} currUserId
 * @param {VotesMap} votes
 * @param {AttendanceMap | null | undefined} attendance
 * @param {(pubId: string, userId: string, status: AttendanceStatus) => Promise<void>} setAttendanceStatus
 * @param {(pubId: string, userId: string) => Promise<void>} clearAttendance
 * @param {(pubId: string, userId: string) => Promise<void>} makeVote
 * @param {(pubId: string, userId: string) => Promise<void>} clearVote
 * @param {string} pollId
 * @returns {VotableRowState}
 */
export function useVotableRow(pubId, currUserId, votes, attendance, setAttendanceStatus, clearAttendance, makeVote, clearVote, pollId) {
  const safeUserId = currUserId || "";

  // Vote-related derived state
  const voteCount = pubId in votes ? votes[pubId].length : 0;
  const votedFor = pubId in votes && votes[pubId].includes(safeUserId);

  // Attendance-related derived state
  const attendanceForPub = getEffectiveAttendanceState(attendance, pubId, currUserId);
  const canCome = attendanceForPub.canCome;
  const cannotCome = attendanceForPub.cannotCome;
  const userCanCome = attendanceForPub.userCanCome;
  const userCannotCome = attendanceForPub.userCannotCome;

  // Control availability
  const canVote = Boolean(currUserId);
  const allowAttendanceControls = canVote && pubId !== "any";
  const allowGlobalAttendanceControls = canVote && pubId === "any";
  const hasAttendanceData = voteCount > 0 || attendanceForPub.hasAttendanceData;

  // Vote handler
  const voteHandler = useCallback(async () => {
    if (!currUserId) return;
    if (votedFor) {
      await clearVote(pubId, currUserId);
    } else {
      await makeVote(pubId, currUserId);
    }
  }, [makeVote, clearVote, pubId, currUserId, votedFor]);

  // Attendance status handler
  /** @type {(status: AttendanceStatus) => Promise<void>} */
  const setAttendanceStatusHandler = useCallback(async (status) => {
    if (!currUserId || pubId === "any") return;
    await runAttendanceAction(() => setAttendanceStatus(pubId, currUserId, status));
  }, [currUserId, pubId, setAttendanceStatus]);

  // Clear attendance handler
  const clearAttendanceHandler = useCallback(async () => {
    if (!currUserId || pubId === "any") return;
    await runAttendanceAction(() => clearAttendance(pubId, currUserId));
  }, [clearAttendance, currUserId, pubId]);

  // Delete pub from poll handler
  const deleteHandler = useCallback(async () => {
    try {
      await deletePubFromPoll(pollId, pubId);
    } catch (error) {
      notifyError(getUserFacingErrorMessage(error, "Unable to remove this pub from the poll."));
    }
  }, [pollId, pubId]);

  return {
    // Vote state
    voteCount,
    votedFor,
    // Attendance state
    userCanCome,
    userCannotCome,
    canCome,
    cannotCome,
    // Control availability
    canVote,
    allowAttendanceControls,
    allowGlobalAttendanceControls,
    hasAttendanceData,
    // Handlers
    voteHandler,
    setAttendanceStatusHandler,
    clearAttendanceHandler,
    deleteHandler,
  };
}
