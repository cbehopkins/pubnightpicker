import { useState, useCallback } from "react";
import { useSelector } from "react-redux";
import { useSearchParams } from "react-router-dom";
import { usePastCompletePolls, useFutureCompletePolls } from "../../hooks/usePolls";
import usePubs from "../../hooks/usePubs";
import useVotes from "../../hooks/useVotes";
import useAttendance from "../../hooks/useAttendance";
import useRole from "../../hooks/useRole";
import { useReschedulePoll } from "../../hooks/useReschedulePoll";
import { useEventAttendance } from "../../hooks/useEventAttendance";
import styles from "./CurrentEvents.module.css";
import ShowAttendance from "../UI/ShowAttendance";
import AttendanceActions from "../UI/AttendanceActions";
import ConfirmModal, { QuestionRender } from "../UI/ConfirmModal";
import ReschedulePollModal from "./ReschedulePollModal";
import { deletePoll } from "../../dbtools/polls";
import { getUserFacingErrorMessage } from "../../permissions";
import { notifyError } from "../../utils/notify";
import { buildCurrentEventViewModel } from "../../utils/currentEventViewModel";

function PastEvent({ value, pub_parameters }) {
  const [imageLoaded, setImageLoaded] = useState(false);
  
  if (!pub_parameters[value.selected]) {
    return <div></div>;
  }
  const pubName = pub_parameters[value.selected].name;
  const pubWebsite = pub_parameters[value.selected]?.web_site
  const pubImage = pub_parameters[value.selected]?.pubImage
  
  const handleImageLoad = () => {
    setImageLoaded(true);
  };
  
  const handleImageError = () => {
    setImageLoaded(false);
  };
  
  return (
    <>
      <h2>{pubName}</h2>
      <h3>{value.date}</h3>
      {/* Open link in new window with _blank magic */}
      {pubWebsite && <p><a href={pubWebsite} target="_blank" rel="noreferrer">Pub Website</a></p>}
      {pubImage && (
        <img
          src={pubImage}
          alt="What the pub looks like"
          className={styles.image}
          onLoad={handleImageLoad}
          onError={handleImageError}
        />
      )}
    </>
  );
}

export function PastEvents() {
  const [searchParams, setSearchParams] = useSearchParams();
  const pubCountParam = Number(searchParams.get("pastPageSize"));
  const pubCount = [5, 10, 20].includes(pubCountParam) ? pubCountParam : 5;
  const cursorTrail = (searchParams.get("pastCursorTrail") || "")
    .split(",")
    .filter(Boolean);
  const currentCursorId = cursorTrail[cursorTrail.length - 1] || null;

  const {
    pollData,
    hasNextPage,
    lastVisibleId,
    isLoading,
  } = usePastCompletePolls(pubCount, currentCursorId);

  const hasPreviousPage = cursorTrail.length > 0;
  const pageIndex = cursorTrail.length;
  const pub_parameters = usePubs();
  const sortedPollsByDate = [...pollData.sortedByDate(true)]

  const writePaginationParams = useCallback((nextPageSize, nextCursorTrail) => {
    const nextParams = new URLSearchParams(searchParams);
    nextParams.set("pastPageSize", String(nextPageSize));
    if (nextCursorTrail.length > 0) {
      nextParams.set("pastCursorTrail", nextCursorTrail.join(","));
    } else {
      nextParams.delete("pastCursorTrail");
    }
    setSearchParams(nextParams);
  }, [searchParams, setSearchParams]);

  const selectPubCountHandler = useCallback(
    (event) => {
      event.preventDefault();
      writePaginationParams(Number(event.target.value), []);
    },
    [writePaginationParams]
  );

  const goToNextPage = useCallback(() => {
    if (!hasNextPage || !lastVisibleId || isLoading) {
      return;
    }
    writePaginationParams(pubCount, [...cursorTrail, lastVisibleId]);
  }, [hasNextPage, lastVisibleId, isLoading, writePaginationParams, pubCount, cursorTrail]);

  const goToPreviousPage = useCallback(() => {
    if (!hasPreviousPage || isLoading) {
      return;
    }
    writePaginationParams(pubCount, cursorTrail.slice(0, -1));
  }, [hasPreviousPage, isLoading, writePaginationParams, pubCount, cursorTrail]);

  return (
    <div>
      <h1>Past Events</h1>
      <span>
        <label>Number of past events to show</label>
        <select value={pubCount} onChange={selectPubCountHandler}>
          <option>5</option>
          <option>10</option>
          <option>20</option>
        </select></span>
      <div className={styles.button}>
        <button type="button" onClick={goToPreviousPage} disabled={!hasPreviousPage || isLoading}>
          Newer Events
        </button>
        <button type="button" onClick={goToNextPage} disabled={!hasNextPage || isLoading}>
          Older Events
        </button>
      </div>
      <p>Page {pageIndex + 1}</p>
      {sortedPollsByDate.map(([key, value]) => {
        return <PastEvent key={key} value={value} pub_parameters={pub_parameters} />
      })}
    </div>
  );
}
function CurrentEvent({ poll_id, current_pub_id, restaurant_id, restaurant_time, date, pub_parameters, can_reschedule, can_delete_event, show_voters, on_open_reschedule }) {
  const currUserId = useSelector((state) => state.auth.uid);
  const [votes] = useVotes(poll_id);
  const [attendance, setAttendanceStatus, clearAttendance] = useAttendance(poll_id);
  const eventViewModel = buildCurrentEventViewModel({
    current_pub_id,
    restaurant_id,
    restaurant_time,
    pub_parameters,
    votes,
    attendance,
    currUserId,
    show_voters,
  });

  if (!eventViewModel) {
    return <div></div>;
  }
  const { mainVenue, restaurantVenue } = eventViewModel;

  // Get attendance handlers for both venues
  const attendanceHandlers = useEventAttendance(
    currUserId,
    setAttendanceStatus,
    clearAttendance,
    mainVenue.id,
    restaurantVenue?.id
  );

  return (
    <>
      {mainVenue.website ? <h2><a href={mainVenue.website} target="_blank" rel="noreferrer">{mainVenue.name}</a></h2> : <h2>{mainVenue.name}</h2>}
      <h3>{date}</h3>
      {mainVenue.address && <p>{mainVenue.address}</p>}
      {mainVenue.image && <p><img
        src={mainVenue.image}
        alt="What the venue looks like"
        className={styles.image}
      /></p>}
      {currUserId && <AttendanceActions
        className={styles.attendanceActions}
        buttonClassName={styles.attendanceButton}
        selectedClassName={styles.attendanceButtonSelected}
        canComeSelected={mainVenue.userCanCome}
        cannotComeSelected={mainVenue.userCannotCome}
        canComeSelectedLabel="Can come confirmed"
        cannotComeSelectedLabel="Cannot come confirmed"
        clearMode="button"
        onSetStatus={attendanceHandlers.setMainAttendanceStatus}
        onClear={attendanceHandlers.clearMainAttendance}
      />}
      {mainVenue.allowShowVoters && <QuestionRender className={styles.button} question="Show venue attendance">
        <ShowAttendance voters={mainVenue.dedupedVotes} canCome={mainVenue.canCome} cannotCome={mainVenue.cannotCome} />
      </QuestionRender>}

      {restaurantVenue && <>
        <h3>Restaurant: {restaurantVenue.name}{restaurantVenue.restaurantTime ? ` (${restaurantVenue.restaurantTime})` : ""}</h3>
        {restaurantVenue.address && <p>{restaurantVenue.address}</p>}
        {currUserId && <AttendanceActions
          className={styles.attendanceActions}
          buttonClassName={styles.attendanceButton}
          selectedClassName={styles.attendanceButtonSelected}
          canComeSelected={restaurantVenue.userCanCome}
          cannotComeSelected={restaurantVenue.userCannotCome}
          canComeSelectedLabel="Can come confirmed"
          cannotComeSelectedLabel="Cannot come confirmed"
          clearMode="button"
          onSetStatus={attendanceHandlers.setRestaurantAttendanceStatus}
          onClear={attendanceHandlers.clearRestaurantAttendance}
        />}
        {restaurantVenue.allowShowVoters && <QuestionRender className={styles.button} question="Show restaurant attendance">
          <ShowAttendance
            voters={restaurantVenue.dedupedVotes}
            canCome={restaurantVenue.canCome}
            cannotCome={restaurantVenue.cannotCome}
          />
        </QuestionRender>}
      </>}

      {can_reschedule && (
        <div className={styles.button}>
          <button
            onClick={() => on_open_reschedule(poll_id, current_pub_id, restaurant_id, restaurant_time)}
          >
            Reschedule Event
          </button>
        </div>
      )}
      {/* Note confirm and cancel look back to front to get the correct button colouring */}
      {can_delete_event && <QuestionRender
        className={styles.button}
        question="Delete This Event"
      >
        <ConfirmModal title="Delete Current event"
          detail="The current event will be deleted"
          confirm_text="Do Nothing"
          cancel_text="Delete it"
          on_cancel={async () => {
            try {
              await deletePoll(poll_id)
            } catch (error) {
              notifyError(getUserFacingErrorMessage(error, "Unable to delete this event."));
            }
          }}
        />
      </QuestionRender>}

    </>
  );
}

function CurrentEvents() {
  const pollData = useFutureCompletePolls();
  const pub_parameters = usePubs();
  const canReschedule = useRole("canCompletePoll");
  const canDeleteEvent = useRole("canCreatePoll");
  const canShowVoters = useRole("canShowVoters");
  const rescheduleState = useReschedulePoll(pub_parameters, canReschedule);

  return (
    <div>
      <h1>Current Events</h1>
      {rescheduleState.isRescheduling && (
        <ReschedulePollModal
          selectedPubId={rescheduleState.selectedPubId}
          pubHasFood={rescheduleState.pubHasFood}
          pubOptions={rescheduleState.pubOptions}
          restaurantOptions={rescheduleState.restaurantOptions}
          chosenRestaurantId={rescheduleState.chosenRestaurantId}
          restaurantTime={rescheduleState.restaurantTime}
          onPubChange={rescheduleState.setSelectedPub}
          onRestaurantChange={rescheduleState.setRestaurantChoice}
          onRestaurantTimeChange={rescheduleState.setRestaurantTime}
          onConfirm={rescheduleState.saveReschedule}
          onCancel={rescheduleState.cancelReschedule}
        />
      )}
      {[...pollData.sortedByDate()].map(([key, value]) => {
        return (
          <CurrentEvent
            key={key}
            poll_id={key}
            current_pub_id={value.selected}
            restaurant_id={value.restaurant}
            restaurant_time={value.restaurant_time}
            date={value.date}
            pub_parameters={pub_parameters}
            can_reschedule={canReschedule}
            can_delete_event={canDeleteEvent}
            show_voters={canShowVoters}
            on_open_reschedule={rescheduleState.openRescheduleModal}
          />
        );
      })}
    </div>
  );
}
export default CurrentEvents;
