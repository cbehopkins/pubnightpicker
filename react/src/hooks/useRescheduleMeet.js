import { useCallback, useState } from "react";
import { rescheduleMeet } from "../dbtools/polls";
import { getUserFacingErrorMessage } from "../permissions";
import { notifyError } from "../utils/notify";
import {
  getAllMainVenueOptions,
  getAllRestaurantVenues,
  getDefaultRestaurantTime,
} from "../utils/venueSelection";

function createReschedulingMeetState(meetId, currentVenueId, restaurantId, restaurantTime, venues) {
  return {
    meetId,
    currentVenueId,
    selectedVenueId: currentVenueId || "",
    mainVenueOptions: getAllMainVenueOptions(venues),
    restaurantOptions: getAllRestaurantVenues(venues),
    restaurantId: restaurantId || "",
    restaurantTime: getDefaultRestaurantTime(restaurantId, restaurantTime || ""),
  };
}

export function useRescheduleMeet(venues, canReschedule) {
  const [reschedulingMeet, setReschedulingMeet] = useState(null);

  const openRescheduleModal = useCallback((meetId, currentVenueId, restaurantId, restaurantTime) => {
    if (!canReschedule) {
      return;
    }

    setReschedulingMeet(
      createReschedulingMeetState(meetId, currentVenueId, restaurantId, restaurantTime, venues)
    );
  }, [canReschedule, venues]);

  const setSelectedVenue = useCallback((venueId) => {
    setReschedulingMeet((prev) => {
      if (!prev) {
        return prev;
      }

      return {
        ...prev,
        selectedVenueId: venueId,
      };
    });
  }, []);

  const setRestaurantChoice = useCallback((restaurantId) => {
    setReschedulingMeet((prev) => {
      if (!prev) {
        return prev;
      }

      return {
        ...prev,
        restaurantId,
        restaurantTime: getDefaultRestaurantTime(restaurantId, prev.restaurantTime),
      };
    });
  }, []);

  const setRestaurantTime = useCallback((restaurantTime) => {
    setReschedulingMeet((prev) => {
      if (!prev) {
        return prev;
      }

      return {
        ...prev,
        restaurantTime,
      };
    });
  }, []);

  const cancelReschedule = useCallback(() => {
    setReschedulingMeet(null);
  }, []);

  const saveReschedule = useCallback(async () => {
    if (!canReschedule || !reschedulingMeet?.meetId || !reschedulingMeet?.selectedVenueId) {
      return;
    }

    try {
      await rescheduleMeet(
        reschedulingMeet.meetId,
        reschedulingMeet.currentVenueId,
        reschedulingMeet.selectedVenueId,
        reschedulingMeet.restaurantId || undefined,
        reschedulingMeet.restaurantId ? reschedulingMeet.restaurantTime : undefined,
      );
      setReschedulingMeet(null);
    } catch (error) {
      notifyError(getUserFacingErrorMessage(error, "Unable to reschedule this meet."));
    }
  }, [canReschedule, reschedulingMeet]);

  const selectedVenueId = reschedulingMeet?.selectedVenueId || "";
  const selectedVenueHasFood = Boolean(selectedVenueId && venues?.[selectedVenueId]?.food);

  return {
    reschedulingMeet,
    selectedVenueId,
    selectedVenueHasFood,
    mainVenueOptions: reschedulingMeet?.mainVenueOptions || [],
    restaurantOptions: reschedulingMeet?.restaurantOptions || [],
    chosenRestaurantId: reschedulingMeet?.restaurantId || "",
    restaurantTime: reschedulingMeet?.restaurantTime || "",
    isRescheduling: Boolean(reschedulingMeet),
    openRescheduleModal,
    setSelectedVenue,
    setRestaurantChoice,
    setRestaurantTime,
    cancelReschedule,
    saveReschedule,
  };
}
