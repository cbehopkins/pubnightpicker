import { useCallback } from "react";
import { deletePubFromPoll } from "../dbtools/polls";
import { getUserFacingErrorMessage } from "../permissions";
import { notifyError } from "../utils/notify";
import { runAttendanceAction } from "../utils/asyncErrorHandler";

/**
 * Hook that encapsulates all the logic for a single votable row in a poll
 * Manages vote state, attendance state, and associated handlers
 */
export function useVotableRow(pubId, currUserId, votes, attendance, setAttendanceStatus, clearAttendance, makeVote, clearVote, pollId) {
  // Vote-related derived state
  const voteCount = pubId in votes ? votes[pubId].length : 0;
  const votedFor = pubId in votes && votes[pubId].includes(currUserId);

  // Attendance-related derived state
  const attendanceForPub = attendance[pubId] || {};
  const canCome = attendanceForPub.canCome || [];
  const cannotCome = attendanceForPub.cannotCome || [];
  const userCanCome = Boolean(currUserId) && canCome.includes(currUserId);
  const userCannotCome = Boolean(currUserId) && cannotCome.includes(currUserId);

  // Control availability
  const canVote = Boolean(currUserId);
  const allowAttendanceControls = canVote && pubId !== "any";
  const allowGlobalAttendanceControls = canVote && pubId === "any";
  const hasAttendanceData = voteCount > 0 || canCome.length > 0 || cannotCome.length > 0;

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
