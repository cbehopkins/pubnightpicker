import { useCallback, useState } from "react";
import { reschedule_a_poll } from "../dbtools/polls";
import { getUserFacingErrorMessage } from "../permissions";
import { notifyError } from "../utils/notify";
import {
  getAllMainVenueOptions,
  getAllRestaurantVenues,
  getDefaultRestaurantTime,
} from "../utils/venueSelection";

function createReschedulingPollState(pollId, currentVenueId, restaurantId, restaurantTime, venues) {
  return {
    pollId,
    currentVenueId,
    selectedVenueId: currentVenueId || "",
    venueOptions: getAllMainVenueOptions(venues),
    restaurantOptions: getAllRestaurantVenues(venues),
    restaurantId: restaurantId || "",
    restaurantTime: getDefaultRestaurantTime(restaurantId, restaurantTime || ""),
  };
}

export function useReschedulePoll(venues, canReschedule) {
  const [reschedulingPoll, setReschedulingPoll] = useState(null);

  const openRescheduleModal = useCallback((pollId, currentVenueId, restaurantId, restaurantTime) => {
    if (!canReschedule) {
      return;
    }

    setReschedulingPoll(
      createReschedulingPollState(pollId, currentVenueId, restaurantId, restaurantTime, venues)
    );
  }, [canReschedule, venues]);

  const setSelectedVenue = useCallback((venueId) => {
    setReschedulingPoll((prev) => {
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
    setReschedulingPoll((prev) => {
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
    setReschedulingPoll((prev) => {
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
    setReschedulingPoll(null);
  }, []);

  const saveReschedule = useCallback(async () => {
    if (!canReschedule || !reschedulingPoll?.pollId || !reschedulingPoll?.selectedVenueId) {
      return;
    }

    try {
      await reschedule_a_poll(
        reschedulingPoll.pollId,
        reschedulingPoll.currentVenueId,
        reschedulingPoll.selectedVenueId,
        reschedulingPoll.restaurantId || undefined,
        reschedulingPoll.restaurantId ? reschedulingPoll.restaurantTime : undefined,
      );
      setReschedulingPoll(null);
    } catch (error) {
      notifyError(getUserFacingErrorMessage(error, "Unable to reschedule this meet."));
    }
  }, [canReschedule, reschedulingPoll]);

  const selectedVenueId = reschedulingPoll?.selectedVenueId || "";
  const venueHasFood = Boolean(selectedVenueId && venues?.[selectedVenueId]?.food);

  return {
    reschedulingPoll,
    selectedVenueId,
    venueHasFood,
    venueOptions: reschedulingPoll?.venueOptions || [],
    restaurantOptions: reschedulingPoll?.restaurantOptions || [],
    chosenRestaurantId: reschedulingPoll?.restaurantId || "",
    restaurantTime: reschedulingPoll?.restaurantTime || "",
    isRescheduling: Boolean(reschedulingPoll),
    openRescheduleModal,
    setSelectedVenue,
    setRestaurantChoice,
    setRestaurantTime,
    cancelReschedule,
    saveReschedule,
  };
}
