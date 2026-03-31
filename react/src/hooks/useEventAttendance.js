import { useCallback } from "react";
import { runAttendanceAction } from "../utils/asyncErrorHandler";

/**
 * Hook that consolidates attendance handlers for a current event
 * Handles attendance for both main venue and restaurant venue
 * Returns: handlers for setting/clearing attendance on both venues
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
