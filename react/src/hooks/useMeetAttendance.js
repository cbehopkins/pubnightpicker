// @ts-check

import { useCallback } from "react";
import { runAttendanceAction } from "../utils/asyncErrorHandler";

/** @typedef {"canCome" | "cannotCome"} AttendanceStatus */

/**
 * @typedef {Object} MeetAttendanceHandlers
 * @property {(status: AttendanceStatus) => Promise<void>} setMainAttendanceStatus
 * @property {() => Promise<void>} clearMainAttendance
 * @property {(status: AttendanceStatus) => Promise<void>} setRestaurantAttendanceStatus
 * @property {() => Promise<void>} clearRestaurantAttendance
 * @property {(eta: string) => Promise<void>} setMainEta
 * @property {() => Promise<void>} clearMainEta
 * @property {(eta: string) => Promise<void>} setRestaurantEta
 * @property {() => Promise<void>} clearRestaurantEta
 */

/**
 * Hook that consolidates attendance handlers for a current Meet
 * Handles attendance for both main venue and restaurant venue
 * Returns: handlers for setting/clearing attendance on both venues
 * @param {string | null | undefined} currUserId
 * @param {(venueId: string, userId: string, status: AttendanceStatus) => Promise<void>} setAttendanceStatus
 * @param {(venueId: string, userId: string) => Promise<void>} clearAttendance
 * @param {string} mainVenueId
 * @param {string | null | undefined} restaurantVenueId
 * @param {(venueId: string, userId: string, eta: string) => Promise<void>} setEta
 * @param {(venueId: string, userId: string) => Promise<void>} clearEta
 * @returns {MeetAttendanceHandlers}
 */
export function useMeetAttendance(
  currUserId,
  setAttendanceStatus,
  clearAttendance,
  mainVenueId,
  restaurantVenueId,
  setEta,
  clearEta
) {
  // Main venue handlers
  const setMainAttendanceStatus = useCallback(
    /** @type {(status: AttendanceStatus) => Promise<void>} */
    async (status) => {
      if (!currUserId) {
        return;
      }

      await setAttendanceStatus(mainVenueId, currUserId, status);
    },
    [currUserId, mainVenueId, setAttendanceStatus]
  );

  const clearMainAttendance = useCallback(
    async () => {
      if (!currUserId) {
        return;
      }

      await clearAttendance(mainVenueId, currUserId);
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

      await setAttendanceStatus(restaurantVenueId, currUserId, status);
    },
    [currUserId, restaurantVenueId, setAttendanceStatus]
  );

  const clearRestaurantAttendance = useCallback(
    async () => {
      if (!currUserId || !restaurantVenueId) {
        return;
      }

      await clearAttendance(restaurantVenueId, currUserId);
    },
    [currUserId, restaurantVenueId, clearAttendance]
  );

  return {
    setMainAttendanceStatus,
    clearMainAttendance,
    setRestaurantAttendanceStatus,
    clearRestaurantAttendance,
    setMainEta: useCallback(
      /** @type {(eta: string) => Promise<void>} */
      async (eta) => {
        if (!currUserId) return;
        await runAttendanceAction(() => setEta(mainVenueId, currUserId, eta));
      },
      [currUserId, mainVenueId, setEta]
    ),
    clearMainEta: useCallback(
      async () => {
        if (!currUserId) return;
        await runAttendanceAction(() => clearEta(mainVenueId, currUserId), "Unable to clear your ETA.");
      },
      [currUserId, mainVenueId, clearEta]
    ),
    setRestaurantEta: useCallback(
      /** @type {(eta: string) => Promise<void>} */
      async (eta) => {
        if (!currUserId || !restaurantVenueId) return;
        await runAttendanceAction(() => setEta(restaurantVenueId, currUserId, eta));
      },
      [currUserId, restaurantVenueId, setEta]
    ),
    clearRestaurantEta: useCallback(
      async () => {
        if (!currUserId || !restaurantVenueId) return;
        await runAttendanceAction(() => clearEta(restaurantVenueId, currUserId), "Unable to clear your restaurant ETA.");
      },
      [currUserId, restaurantVenueId, clearEta]
    ),
  };
}
