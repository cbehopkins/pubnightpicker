// @ts-check

import { useCallback } from "react";
import { runAttendanceAction } from "../utils/asyncErrorHandler";

/** @typedef {"canCome" | "cannotCome"} AttendanceStatus */

/**
 * @typedef {Object} EventAttendanceHandlers
 * @property {(status: AttendanceStatus) => Promise<void>} setMainAttendanceStatus
 * @property {() => Promise<void>} clearMainAttendance
 * @property {(status: AttendanceStatus) => Promise<void>} setRestaurantAttendanceStatus
 * @property {() => Promise<void>} clearRestaurantAttendance
 */

/**
 * Hook that consolidates attendance handlers for a current event
 * Handles attendance for both main venue and restaurant venue
 * Returns: handlers for setting/clearing attendance on both venues
 * @param {string | null | undefined} currUserId
 * @param {(pubId: string, userId: string, status: AttendanceStatus) => Promise<void>} setAttendanceStatus
 * @param {(pubId: string, userId: string) => Promise<void>} clearAttendance
 * @param {string} mainVenueId
 * @param {string | null | undefined} restaurantVenueId
 * @returns {EventAttendanceHandlers}
 */
export function useEventAttendance(
  currUserId,
  setAttendanceStatus,
  clearAttendance,
  mainVenueId,
  restaurantVenueId
) {
  // Main venue handlers
  const setMainAttendanceStatus = useCallback(
    /** @type {(status: AttendanceStatus) => Promise<void>} */
    async (status) => {
      if (!currUserId) {
        return;
      }

      await runAttendanceAction(() => setAttendanceStatus(mainVenueId, currUserId, status));
    },
    [currUserId, mainVenueId, setAttendanceStatus]
  );

  const clearMainAttendance = useCallback(
    async () => {
      if (!currUserId) {
        return;
      }

      await runAttendanceAction(
        () => clearAttendance(mainVenueId, currUserId),
        "Unable to clear your attendance."
      );
    },
    [currUserId, mainVenueId, clearAttendance]
  );

  // Restaurant venue handlers
  const setRestaurantAttendanceStatus = useCallback(
    /** @type {(status: AttendanceStatus) => Promise<void>} */
    async (status) => {
      if (!currUserId || !restaurantVenueId) {
        return;
      }

      await runAttendanceAction(() => setAttendanceStatus(restaurantVenueId, currUserId, status));
    },
    [currUserId, restaurantVenueId, setAttendanceStatus]
  );

  const clearRestaurantAttendance = useCallback(
    async () => {
      if (!currUserId || !restaurantVenueId) {
        return;
      }

      await runAttendanceAction(
        () => clearAttendance(restaurantVenueId, currUserId),
        "Unable to clear your restaurant attendance."
      );
    },
    [currUserId, restaurantVenueId, clearAttendance]
  );

  return {
    setMainAttendanceStatus,
    clearMainAttendance,
    setRestaurantAttendanceStatus,
    clearRestaurantAttendance,
  };
}
