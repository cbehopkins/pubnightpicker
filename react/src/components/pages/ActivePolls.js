import React from "react";
import styles from "./ActivePolls.module.css";
import usePolls from "../../hooks/usePolls";
import usePubs from "../../hooks/usePubs";
import ActivePoll from "../UI/ActivePoll";
import NewPoll from "../UI/NewPoll";
import useRole from "../../hooks/useRole";
import { useIsMobileView } from "../../hooks/useIsMobileView";
import { useCompletePolls } from "../../hooks/useCompletePolls";
import CompletePollModal from "./CompletePollModal";

function ActivePolls() {
  const mobile = useIsMobileView();
  const canCreatePoll = useRole("canCreatePoll");
  const canCompletePoll = useRole("canCompletePoll");

  const pollData = usePolls();
  const pubs = usePubs();
  const currentPollDates = pollData.dates;

  // Get poll completion workflow hooks
  const completePollsState = useCompletePolls(pollData, pubs, canCompletePoll);

  const styleToUse = mobile ? styles.poll_mobile : styles.poll;

  return (
    <div>
      {canCreatePoll && <NewPoll polls={currentPollDates} />}
      <h1>Active Polls</h1>
      {completePollsState.isCompletingPollBusy && (
        <CompletePollModal
          pubName={completePollsState.pubName}
          restaurantOptions={completePollsState.restaurantOptions}
          chosenRestaurantId={completePollsState.chosenRestaurantId}
          restaurantTime={completePollsState.restaurantTime}
          hasRestaurantAssociation={completePollsState.hasRestaurantAssociation}
          restaurantChoiceRequired={completePollsState.restaurantChoiceRequired}
          onRestaurantChange={completePollsState.setRestaurantChoice}
          onRestaurantTimeChange={completePollsState.setRestaurantTime}
          onConfirm={completePollsState.completeThePoll}
          onCancel={completePollsState.cancelCompletion}
        />
      )}
      {/* js needs to die, and have a very cheap funeral!!! */}
      {[...pollData.sortedByDate()].map(([id, poll]) => (
        <div className={styleToUse} key={id}>
          <ActivePoll
            poll_id={id}
            pub_parameters={pubs}
            poll_data={poll}
            on_complete={completePollsState.completeHandler}
            mobile={mobile}
          />
        </div>
      ))}
    </div>
  );
}
export default ActivePolls;
