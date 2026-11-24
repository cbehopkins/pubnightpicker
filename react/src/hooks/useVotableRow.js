// @ts-check

import { useCallback } from "react";
import { removeVenueFromMeet } from "../dbtools/polls";
import { getUserFacingErrorMessage } from "../permissions";
import { notifyError } from "../utils/notify";
import { runAttendanceAction } from "../utils/asyncErrorHandler";
import { getEffectiveAttendanceState } from "../utils/attendanceState";

/** @typedef {"canCome" | "cannotCome"} AttendanceStatus */
/** @typedef {{ total: number, voterUids: string[] }} VotingOption */
/** @typedef {Record<string, VotingOption | undefined>} VotesMap */
/** @typedef {Record<string, { canCome?: string[], cannotCome?: string[], eta?: Record<string, string> } | undefined>} AttendanceMap */

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
 * @property {string | undefined} userEta
 * @property {() => Promise<boolean>} voteHandler
 * @property {(status: AttendanceStatus) => Promise<void>} setAttendanceStatusHandler
 * @property {() => Promise<void>} clearAttendanceHandler
 * @property {(eta: string) => Promise<void>} setEtaHandler
 * @property {() => Promise<void>} clearEtaHandler
 * @property {() => Promise<void>} deleteHandler
 * @property {boolean} votePending
 * @property {boolean | undefined} retrySelection
 * @property {() => Promise<boolean>} retryVoteHandler
 */

/**
 * Hook that encapsulates all the logic for a single votable row in a poll
 * Manages vote state, attendance state, and associated handlers
 * @param {string} venueId
 * @param {string | null | undefined} currUserId
 * @param {VotesMap} votes
 * @param {string[]} selectedOptionIds
 * @param {string[]} pendingOptionIds
 * @param {Record<string, boolean>} retrySelections
 * @param {AttendanceMap | null | undefined} attendance
 * @param {(venueId: string, userId: string, status: AttendanceStatus) => Promise<void>} setAttendanceStatus
 * @param {(venueId: string, userId: string) => Promise<void>} clearAttendance
 * @param {(venueId: string, selected: boolean) => Promise<boolean>} changeVote
 * @param {(venueId: string) => Promise<boolean>} retryVote
 * @param {(venueId: string, userId: string, eta: string) => Promise<void>} setEta
 * @param {(venueId: string, userId: string) => Promise<void>} clearEta
 * @param {string} pollId
 * @param {string} [venueType="pub"]
 * @returns {VotableRowState}
 */
export function useVotableRow(venueId, currUserId, votes, selectedOptionIds, pendingOptionIds, retrySelections, attendance, setAttendanceStatus, clearAttendance, changeVote, retryVote, setEta, clearEta, pollId, venueType = "pub") {
  // Vote-related derived state
  const voteCount = votes[venueId]?.total || 0;
  const votedFor = selectedOptionIds.includes(venueId);
  const votePending = pendingOptionIds.includes(venueId);
  const retrySelection = retrySelections[venueId];

  // Attendance-related derived state
  const attendanceForVenue = getEffectiveAttendanceState(attendance, venueId, currUserId, venueType);
  const canCome = attendanceForVenue.canCome;
  const cannotCome = attendanceForVenue.cannotCome;
  const userCanCome = attendanceForVenue.userCanCome;
  const userCannotCome = attendanceForVenue.userCannotCome;
  const userEta = attendanceForVenue.userEta;

  // Control availability
  const canVote = Boolean(currUserId);
  const allowAttendanceControls = canVote && venueId !== "any";
  const allowGlobalAttendanceControls = canVote && venueId === "any";
  const hasAttendanceData = voteCount > 0 || attendanceForVenue.hasAttendanceData;

  // Vote handler
  const voteHandler = useCallback(async () => {
    if (!currUserId) return false;
    return changeVote(venueId, !votedFor);
  }, [changeVote, venueId, currUserId, votedFor]);

  const retryVoteHandler = useCallback(async () => retryVote(venueId), [retryVote, venueId]);

  // Attendance status handler
  /** @type {(status: AttendanceStatus) => Promise<void>} */
  const setAttendanceStatusHandler = useCallback(async (status) => {
    if (!currUserId || venueId === "any") return;
    await setAttendanceStatus(venueId, currUserId, status);
  }, [currUserId, venueId, setAttendanceStatus]);

  // Clear attendance handler
  const clearAttendanceHandler = useCallback(async () => {
    if (!currUserId) return;
    await clearAttendance(venueId, currUserId);
  }, [clearAttendance, currUserId, venueId]);

  // Delete venue from poll handler
  const deleteHandler = useCallback(async () => {
    try {
      await removeVenueFromMeet(pollId, venueId);
    } catch (error) {
      notifyError(getUserFacingErrorMessage(error, "Unable to remove this venue from the poll."));
    }
  }, [pollId, venueId]);

  // ETA handlers
  /** @type {(eta: string) => Promise<void>} */
  const setEtaHandler = useCallback(async (eta) => {
    if (!currUserId || venueId === "any") return;
    await runAttendanceAction(() => setEta(venueId, currUserId, eta));
  }, [currUserId, venueId, setEta]);

  const clearEtaHandler = useCallback(async () => {
    if (!currUserId || venueId === "any") return;
    await runAttendanceAction(() => clearEta(venueId, currUserId));
  }, [clearEta, currUserId, venueId]);

  return {
    // Vote state
    voteCount,
    votedFor,
    votePending,
    retrySelection,
    retryVoteHandler,
    // Attendance state
    userCanCome,
    userCannotCome,
    canCome,
    cannotCome,
    userEta,
    // Control availability
    canVote,
    allowAttendanceControls,
    allowGlobalAttendanceControls,
    hasAttendanceData,
    // Handlers
    voteHandler,
    setAttendanceStatusHandler,
    clearAttendanceHandler,
    setEtaHandler,
    clearEtaHandler,
    deleteHandler,
  };
}
