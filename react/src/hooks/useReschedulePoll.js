import { useCallback, useState } from "react";
import { reschedule_a_poll } from "../dbtools/polls";
import { getUserFacingErrorMessage } from "../permissions";
import { notifyError } from "../utils/notify";
import {
  getAllMainVenueOptions,
  getAllRestaurantVenues,
  getDefaultRestaurantTime,
} from "../utils/venueSelection";

function createReschedulingPollState(pollId, currentPubId, restaurantId, restaurantTime, venues) {
  return {
    pollId,
    currentPubId,
    selectedPubId: currentPubId || "",
    pubOptions: getAllMainVenueOptions(venues),
    restaurantOptions: getAllRestaurantVenues(venues),
    restaurantId: restaurantId || "",
    restaurantTime: getDefaultRestaurantTime(restaurantId, restaurantTime || ""),
  };
}

export function useReschedulePoll(venues, canReschedule) {
  const [reschedulingPoll, setReschedulingPoll] = useState(null);

  const openRescheduleModal = useCallback((pollId, currentPubId, restaurantId, restaurantTime) => {
    if (!canReschedule) {
      return;
    }

    setReschedulingPoll(
      createReschedulingPollState(pollId, currentPubId, restaurantId, restaurantTime, venues)
    );
  }, [canReschedule, venues]);

  const setSelectedPub = useCallback((pubId) => {
    setReschedulingPoll((prev) => {
      if (!prev) {
        return prev;
      }

      return {
        ...prev,
        selectedPubId: pubId,
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
    if (!canReschedule || !reschedulingPoll?.pollId || !reschedulingPoll?.selectedPubId) {
      return;
    }

    try {
      await reschedule_a_poll(
        reschedulingPoll.pollId,
        reschedulingPoll.currentPubId,
        reschedulingPoll.selectedPubId,
        reschedulingPoll.restaurantId || undefined,
        reschedulingPoll.restaurantId ? reschedulingPoll.restaurantTime : undefined,
      );
      setReschedulingPoll(null);
    } catch (error) {
      notifyError(getUserFacingErrorMessage(error, "Unable to reschedule this event."));
    }
  }, [canReschedule, reschedulingPoll]);

  const selectedPubId = reschedulingPoll?.selectedPubId || "";
  const pubHasFood = Boolean(selectedPubId && venues?.[selectedPubId]?.food);

  return {
    reschedulingPoll,
    selectedPubId,
    pubHasFood,
    pubOptions: reschedulingPoll?.pubOptions || [],
    restaurantOptions: reschedulingPoll?.restaurantOptions || [],
    chosenRestaurantId: reschedulingPoll?.restaurantId || "",
    restaurantTime: reschedulingPoll?.restaurantTime || "",
    isRescheduling: Boolean(reschedulingPoll),
    openRescheduleModal,
    setSelectedPub,
    setRestaurantChoice,
    setRestaurantTime,
    cancelReschedule,
    saveReschedule,
  };
}
