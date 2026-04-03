import { useState, useCallback } from "react";
import { complete_a_poll } from "../dbtools/polls";
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
export function useCompletePolls(pollData, pubs, canCompletePoll) {
  const [completingPoll, setCompletingPoll] = useState(null);

  // Handler to initiate poll completion (triggered when user clicks a pub)
  const completeHandler = useCallback(
    (key, pubName, poll_id) => {
      if (!canCompletePoll) {
        return;
      }
      const poll = pollData.polls?.[poll_id];
      setCompletingPoll(createCompletingPollState(key, pubName, poll_id, poll, pubs));
    },
    [canCompletePoll, pollData.polls, pubs]
  );

  // Extract state from completingPoll for easier access
  const key = completingPoll?.key;
  const pubName = completingPoll?.pubName;
  const poll_id = completingPoll?.poll_id;
  const pubHasFood = completingPoll?.pubHasFood ?? false;
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

      try {
        await complete_a_poll(key, poll_id, restaurantToPersist, restaurantTimeToPersist);
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
    pubName,
    pubHasFood,
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
