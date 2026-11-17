import { useState, useCallback } from "react";
import { completeMeetVoting } from "../dbtools/polls";
import { getUserFacingErrorMessage } from "../permissions";
import { notifyError } from "../utils/notify";
import {
  createCompletingPollState,
  getDefaultRestaurantTime,
  getRestaurantIdForCompletion,
} from "../utils/venueSelection";

/**
 * Hook that manages the poll completion workflow
 * Handles: initiating completion, collecting restaurant choice, final completion
 * Returns: state for modal display and handlers for user interactions
 */
export function useCompletePolls(meetData, venues, canCompletePoll) {
  const [completingPoll, setCompletingPoll] = useState(null);

  // Handler to initiate poll completion (triggered when user clicks a venue)
  const completeHandler = useCallback(
    (key, venueName, poll_id) => {
      if (!canCompletePoll) {
        return;
      }
      const meet = meetData.meets?.[poll_id];
      setCompletingPoll(createCompletingPollState(key, venueName, poll_id, meet, venues));
    },
    [canCompletePoll, meetData.meets, venues]
  );

  // Extract state from completingPoll for easier access
  const key = completingPoll?.key;
  const venueName = completingPoll?.venueName;
  const poll_id = completingPoll?.poll_id;
  const venueHasFood = completingPoll?.venueHasFood ?? false;
  const restaurantOptions = completingPoll?.restaurantOptions || [];
  const allRestaurantVenues = completingPoll?.allRestaurantVenues || [];
  const chosenRestaurantId = completingPoll?.restaurantId || "";
  const restaurantTime = completingPoll?.restaurantTime || "";
  // When the poll has no restaurants, fall back to all system restaurants
  const availableRestaurants = restaurantOptions.length > 0 ? restaurantOptions : allRestaurantVenues;
  /** @type {"poll" | "system"} */
  const restaurantSource = restaurantOptions.length > 0 ? "poll" : "system";

  // Handler to actually complete the poll (after modal confirmation)
  const completeThePoll = useCallback(
    async () => {
      if (!canCompletePoll) {
        return;
      }

      if (!key || !poll_id) {
        return;
      }

      const restaurantToPersist = getRestaurantIdForCompletion(completingPoll);
      const restaurantTimeToPersist = restaurantToPersist ? restaurantTime : undefined;
      const pollDate = meetData.meets?.[poll_id]?.date;

      try {
        await completeMeetVoting(
          key,
          poll_id,
          pollDate,
          restaurantToPersist,
          restaurantTimeToPersist
        );
        setCompletingPoll(null);
      } catch (error) {
        notifyError(getUserFacingErrorMessage(error, "Unable to complete this poll."));
      }
    },
    [
      key,
      poll_id,
      canCompletePoll,
      completingPoll,
      restaurantTime,
      meetData.meets,
    ]
  );

  // Handler to update restaurant choice in modal
  const setRestaurantChoice = useCallback((restaurantId) => {
    setCompletingPoll((prev) => {
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

  // Handler to update restaurant time in modal
  const setRestaurantTime = useCallback((timeValue) => {
    setCompletingPoll((prev) => {
      if (!prev) {
        return prev;
      }
      return {
        ...prev,
        restaurantTime: timeValue,
      };
    });
  }, []);

  // Handler to cancel completion flow
  const cancelCompletion = useCallback(() => {
    setCompletingPoll(null);
  }, []);

  return {
    // State for modal display
    completingPoll,
    completingPollId: poll_id,
    venueName,
    venueHasFood,
    availableRestaurants,
    restaurantSource,
    chosenRestaurantId,
    restaurantTime,
    isCompletingPollBusy: Boolean(completingPoll),
    // Handlers
    completeHandler,
    completeThePoll,
    setRestaurantChoice,
    setRestaurantTime,
    cancelCompletion,
  };
}
