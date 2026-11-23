// @ts-check

import styles from "./ActivePolls.module.css";
import { useSelector } from "react-redux";
import { useOpenMeets } from "../../hooks/useMeets";
import useVenues from "../../hooks/useVenues";
import ActivePoll from "../UI/ActivePoll";
import NewPoll from "../UI/NewPoll";
import useRole from "../../hooks/useRole";
import { useIsMobileView } from "../../hooks/useIsMobileView";
import { useCompletePolls } from "../../hooks/useCompletePolls";
import CompletePollModal from "./CompletePollModal";

/** @typedef {{ date?: string, venues?: Record<string, unknown> }} MeetValue */
/** @typedef {{ meets: Record<string, MeetValue>, dates: Set<string>, sortedByDate: () => Iterable<[string, MeetValue]> }} MeetData */
/** @typedef {import("../../store").RootState} RootState */

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
  const groupId = useSelector(
    /** @param {RootState} state */
    (state) => state.group?.groupId ?? null
  );
  const userId = useSelector(
    /** @param {RootState} state */
    (state) => state.auth?.uid ?? ""
  );
  const openMeetsState = useOpenMeets(groupId, userId);
  const meetData = toMeetData(openMeetsState.meetData);
  const venues = useVenues();
  const currentPollDates = meetData.dates;

  // Get poll completion workflow hooks
  const completePollsState = useCompletePolls(meetData, venues, canCompletePoll);

  const styleToUse = mobile ? styles.poll_mobile : styles.poll;

  return (
    <div>
      {canCreatePoll && <NewPoll polls={currentPollDates} />}
      <h1>Active Polls</h1>
      {openMeetsState.isLoading && <p role="status">Loading Open Meets...</p>}
      {openMeetsState.errorMessage && (
        <div className="alert alert-warning" role="alert">
          <p className="mb-2">{openMeetsState.errorMessage}</p>
          {openMeetsState.isStale && <p className="mb-2">Showing the last received snapshot.</p>}
          <button type="button" className="btn btn-outline-secondary btn-sm" onClick={openMeetsState.retry}>
            Retry
          </button>
        </div>
      )}
      {!openMeetsState.isLoading && !openMeetsState.errorMessage && Object.keys(meetData.meets).length === 0 && (
        <p className="text-body-secondary">There are no open Meets for this Group.</p>
      )}
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
