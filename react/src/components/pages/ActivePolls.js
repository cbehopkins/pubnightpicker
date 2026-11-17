// @ts-check

import styles from "./ActivePolls.module.css";
import { useActiveMeets } from "../../hooks/useMeets";
import useVenues from "../../hooks/useVenues";
import ActivePoll from "../UI/ActivePoll";
import NewPoll from "../UI/NewPoll";
import useRole from "../../hooks/useRole";
import { useIsMobileView } from "../../hooks/useIsMobileView";
import { useCompletePolls } from "../../hooks/useCompletePolls";
import CompletePollModal from "./CompletePollModal";

/** @typedef {{ date?: string, venues?: Record<string, unknown> }} MeetValue */
/** @typedef {{ meets: Record<string, MeetValue>, dates: Set<string>, sortedByDate: () => Iterable<[string, MeetValue]> }} MeetData */

/**
 * @param {unknown} value
 * @returns {MeetData}
 */
function toMeetData(value) {
  return /** @type {MeetData} */ (value);
}

function ActivePolls() {
  const mobile = useIsMobileView();
  const canCreatePoll = useRole("canCreatePoll");
  const canCompletePoll = useRole("canCompletePoll");

  const meetData = toMeetData(useActiveMeets());
  const venues = useVenues();
  const currentPollDates = meetData.dates;

  // Get poll completion workflow hooks
  const completePollsState = useCompletePolls(meetData, venues, canCompletePoll);

  const styleToUse = mobile ? styles.poll_mobile : styles.poll;

  return (
    <div>
      {canCreatePoll && <NewPoll polls={currentPollDates} />}
      <h1>Active Polls</h1>
      {completePollsState.isCompletingPollBusy && (
        <CompletePollModal
          pollId={completePollsState.completingPollId}
          venueName={completePollsState.venueName}
          venueHasFood={completePollsState.venueHasFood}
          availableRestaurants={completePollsState.availableRestaurants}
          restaurantSource={completePollsState.restaurantSource}
          chosenRestaurantId={completePollsState.chosenRestaurantId}
          restaurantTime={completePollsState.restaurantTime}
          onRestaurantChange={completePollsState.setRestaurantChoice}
          onRestaurantTimeChange={completePollsState.setRestaurantTime}
          onConfirm={completePollsState.completeThePoll}
          onCancel={completePollsState.cancelCompletion}
        />
      )}
      {/* js needs to die, and have a very cheap funeral!!! */}
      {[...meetData.sortedByDate()].map(([id, meet]) => (
        <div className={styleToUse} key={id}>
          <ActivePoll
            poll_id={id}
            venues={venues}
            poll_data={meet}
            on_complete={completePollsState.completeHandler}
            mobile={mobile}
          />
        </div>
      ))}
    </div>
  );
}
export default ActivePolls;
