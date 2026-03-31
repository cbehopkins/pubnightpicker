import React, { useState, useCallback, useEffect } from "react";
import styles from "./ActivePolls.module.css";
import usePolls from "../../hooks/usePolls";
import usePubs from "../../hooks/usePubs";
import ActivePoll from "../UI/ActivePoll";
import NewPoll from "../UI/NewPoll";
import useRole from "../../hooks/useRole";
import { complete_a_poll } from "../../dbtools/polls";
import { getUserFacingErrorMessage } from "../../permissions";
import { notifyError } from "../../utils/notify";
import CompletePollModal from "./CompletePollModal";
import {
  createCompletingPollState,
  getRestaurantIdForCompletion,
  isRestaurantChoiceRequired,
} from "../../utils/venueSelection";

function ActivePolls() {
  const [mobile, setMobile] = useState(window.innerWidth <= 800);
  const canCreatePoll = useRole("canCreatePoll");
  const canCompletePoll = useRole("canCompletePoll");

  const handleWindowSizeChange = useCallback(() => {
    setMobile(window.innerWidth <= 800);
  }, [setMobile]);

  useEffect(() => {
    window.addEventListener("resize", handleWindowSizeChange);
    // FIXME this listener does not work!
    // I think, Because the new one can be created before the old one is removed
    return () => {
      window.removeEventListener("resize", handleWindowSizeChange);
    };
  }, [handleWindowSizeChange]);

  const pollData = usePolls();
  const pubs = usePubs();
  const currentPollDates = pollData.dates;

  // We have a 2 stage approach, the click handler updates the
  // completingPoll state
  const [completingPoll, setCompletingPoll] = useState(null);
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

  const key = completingPoll?.key;
  const pubName = completingPoll?.pubName;
  const poll_id = completingPoll?.poll_id;
  const restaurantOptions = completingPoll?.restaurantOptions || [];
  const chosenRestaurantId = completingPoll?.restaurantId || "";
  const restaurantTime = completingPoll?.restaurantTime || "";
  const hasRestaurantAssociation = restaurantOptions.length > 0;
  const restaurantChoiceRequired = isRestaurantChoiceRequired(completingPoll);
  // Then if we decide to complete, then actually run the complete
  const completeThePoll = useCallback(async () => {
    if (!canCompletePoll) {
      return;
    }

    if (!key || !poll_id) {
      return;
    }

    if (restaurantChoiceRequired && !chosenRestaurantId) {
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
  }, [
    key,
    poll_id,
    canCompletePoll,
    chosenRestaurantId,
    restaurantChoiceRequired,
    completingPoll,
    restaurantTime,
  ]);

  const completingPollBusy = Boolean(completingPoll);
  const styleToUse = mobile ? styles.poll_mobile : styles.poll;
  const setRestaurantChoice = useCallback((restaurantId) => {
    setCompletingPoll((prev) => {
      if (!prev) {
        return prev;
      }
      return {
        ...prev,
        restaurantId,
      };
    });
  }, []);

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

  // Change line numbers
  return (
    <div>
      {canCreatePoll && <NewPoll polls={currentPollDates} />}
      <h1>Active Polls</h1>
      {completingPollBusy && (
        <CompletePollModal
          pubName={pubName}
          restaurantOptions={restaurantOptions}
          chosenRestaurantId={chosenRestaurantId}
          restaurantTime={restaurantTime}
          hasRestaurantAssociation={hasRestaurantAssociation}
          restaurantChoiceRequired={restaurantChoiceRequired}
          onRestaurantChange={setRestaurantChoice}
          onRestaurantTimeChange={setRestaurantTime}
          onConfirm={completeThePoll}
          onCancel={() => {
            setCompletingPoll(null);
          }}
        />
      )}
      {/* js needs to die, and have a very cheap funeral!!! */}
      {[...pollData.sortedByDate()].map(([id, poll]) => (
        <div className={styleToUse} key={id}>
          <ActivePoll
            poll_id={id}
            pub_parameters={pubs}
            poll_data={poll}
            on_complete={completeHandler}
            mobile={mobile}
          />
        </div>
      ))}
    </div>
  );
}
export default ActivePolls;
