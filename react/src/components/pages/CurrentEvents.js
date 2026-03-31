import { useState, useCallback, useMemo, useEffect } from "react";
import { useSelector } from "react-redux";
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
import Button from "../UI/Button";
import ReschedulePollModal from "./ReschedulePollModal";
import { deletePoll } from "../../dbtools/polls";
import { getUserFacingErrorMessage } from "../../permissions";
import { notifyError } from "../../utils/notify";
import { buildCurrentEventViewModel } from "../../utils/currentEventViewModel";

function PastEvent({ value, pub_parameters }) {
  if (!pub_parameters[value.selected]) {
    return <div></div>;
  }
  const pubName = pub_parameters[value.selected].name;
  const pubWebsite = pub_parameters[value.selected]?.web_site;
  const pubImage = pub_parameters[value.selected]?.pubImage;

  return (
    <div className="col">
      <div className="card shadow-sm h-100">
        {pubImage ? (
          <img
            src={pubImage}
            alt={`Photo of ${pubName}`}
            className={`card-img-top ${styles.pastEventImage}`}
          />
        ) : (
          <div
            className={`${styles.pastEventImagePlaceholder} card-img-top d-flex align-items-center justify-content-center`}
          >
            <span className="text-body-secondary">No image available</span>
          </div>
        )}
        <div className="card-body d-flex flex-column">
          <h5 className="card-title mb-2">{pubName}</h5>
          <p className="card-text text-body-secondary mb-3">{value.date}</p>
          <div className="mt-auto">
            {pubWebsite && (
              <a
                href={pubWebsite}
                target="_blank"
                rel="noreferrer"
                className="btn btn-outline-primary btn-sm"
              >
                Pub website
              </a>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

export function PastEvents() {
  const [pubCount, setPubCount] = useState(5);
  const [pageIndex, setPageIndex] = useState(0);
  const pollData = usePastCompletePolls();
  const pub_parameters = usePubs();
  const allPastPolls = useMemo(() => [...pollData.sortedByDate(true)], [pollData]);

  const pageSize = Number(pubCount);
  const totalPages = Math.max(1, Math.ceil(allPastPolls.length / pageSize));
  const safePageIndex = Math.min(pageIndex, totalPages - 1);
  const startIndex = safePageIndex * pageSize;
  const visiblePolls = allPastPolls.slice(startIndex, startIndex + pageSize);
  const hasPreviousPage = safePageIndex > 0;
  const hasNextPage = startIndex + pageSize < allPastPolls.length;

  useEffect(() => {
    if (pageIndex !== safePageIndex) {
      setPageIndex(safePageIndex);
    }
  }, [pageIndex, safePageIndex]);

  const selectPubCountHandler = useCallback(
    (event) => {
      event.preventDefault();
      setPubCount(event.target.value);
      setPageIndex(0);
    },
    [setPubCount, setPageIndex]
  );

  const goToPreviousPage = useCallback(() => {
    if (hasPreviousPage) {
      setPageIndex((current) => current - 1);
    }
  }, [hasPreviousPage]);

  const goToNextPage = useCallback(() => {
    if (hasNextPage) {
      setPageIndex((current) => current + 1);
    }
  }, [hasNextPage]);

  return (
    <section className="py-4 py-md-5">
      <div className="container">
        <div className="row mb-4 align-items-end g-3">
          <div className="col-md-8">
            <h1 className="h2 mb-2">Past Events</h1>
            <p className="text-body-secondary mb-0">
              Browse previous pub nights in an album-style view.
            </p>
          </div>
          <div className="col-md-4">
            <label htmlFor="pastEventCount" className="form-label">
              Number of past events to show
            </label>
            <select
              id="pastEventCount"
              className="form-select"
              defaultValue={pubCount}
              onChange={selectPubCountHandler}
            >
              <option>5</option>
              <option>10</option>
              <option>20</option>
            </select>
          </div>
        </div>

        <div className="row row-cols-1 row-cols-sm-2 row-cols-lg-3 g-4">
          {visiblePolls.map(([key, value]) => {
            return <PastEvent key={key} value={value} pub_parameters={pub_parameters} />;
          })}
        </div>

        <div className="d-flex align-items-center justify-content-between mt-4 gap-3 flex-wrap">
          <div className="text-body-secondary">Page {safePageIndex + 1} of {totalPages}</div>
          <div className="d-flex gap-2">
            <button
              type="button"
              className="btn btn-outline-secondary"
              onClick={goToPreviousPage}
              disabled={!hasPreviousPage}
            >
              Newer Events
            </button>
            <button
              type="button"
              className="btn btn-outline-secondary"
              onClick={goToNextPage}
              disabled={!hasNextPage}
            >
              Older Events
            </button>
          </div>
        </div>
      </div>
    </section>
  );
}

function CurrentEvent({
  poll_id,
  current_pub_id,
  restaurant_id,
  restaurant_time,
  date,
  pub_parameters,
  can_reschedule,
  can_delete_event,
  show_voters,
  on_open_reschedule,
}) {
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

  const attendanceHandlers = useEventAttendance(
    currUserId,
    setAttendanceStatus,
    clearAttendance,
    mainVenue.id,
    restaurantVenue?.id
  );

  return (
    <article className="card shadow-sm mb-4">
      <div className="card-body">
        <div className="d-flex flex-column flex-lg-row justify-content-between align-items-lg-start gap-3 mb-3">
          <div>
            {mainVenue.website ? (
              <h2 className="h4 mb-1">
                <a href={mainVenue.website} target="_blank" rel="noreferrer" className="link-primary text-decoration-none">
                  {mainVenue.name}
                </a>
              </h2>
            ) : (
              <h2 className="h4 mb-1">{mainVenue.name}</h2>
            )}
            <p className="text-body-secondary mb-0">{date}</p>
          </div>
        </div>

        {mainVenue.address && <p className="mb-3">{mainVenue.address}</p>}

        {mainVenue.image && (
          <div className="mb-3">
            <img
              src={mainVenue.image}
              alt="What the venue looks like"
              className={`img-fluid rounded ${styles.image}`}
            />
          </div>
        )}

        {currUserId && (
          <AttendanceActions
            className={styles.attendanceActions}
            buttonClassName="btn-sm"
            canComeSelected={mainVenue.userCanCome}
            cannotComeSelected={mainVenue.userCannotCome}
            canComeSelectedLabel="Can come confirmed"
            cannotComeSelectedLabel="Cannot come confirmed"
            clearMode="button"
            onSetStatus={attendanceHandlers.setMainAttendanceStatus}
            onClear={attendanceHandlers.clearMainAttendance}
          />
        )}

        {mainVenue.allowShowVoters && (
          <div className="mb-3">
            <QuestionRender className={styles.actionBlock} question="Show venue attendance">
              <ShowAttendance
                voters={mainVenue.dedupedVotes}
                canCome={mainVenue.canCome}
                cannotCome={mainVenue.cannotCome}
              />
            </QuestionRender>
          </div>
        )}

        {restaurantVenue && (
          <section className="border rounded p-3 bg-body-tertiary mb-3">
            <h3 className="h6 mb-2">
              Restaurant: {restaurantVenue.name}
              {restaurantVenue.restaurantTime ? ` (${restaurantVenue.restaurantTime})` : ""}
            </h3>
            {restaurantVenue.address && <p className="mb-3">{restaurantVenue.address}</p>}

            {currUserId && (
              <AttendanceActions
                className={styles.attendanceActions}
                buttonClassName="btn-sm"
                canComeSelected={restaurantVenue.userCanCome}
                cannotComeSelected={restaurantVenue.userCannotCome}
                canComeSelectedLabel="Can come confirmed"
                cannotComeSelectedLabel="Cannot come confirmed"
                clearMode="button"
                onSetStatus={attendanceHandlers.setRestaurantAttendanceStatus}
                onClear={attendanceHandlers.clearRestaurantAttendance}
              />
            )}

            {restaurantVenue.allowShowVoters && (
              <QuestionRender className={styles.actionBlock} question="Show restaurant attendance">
                <ShowAttendance
                  voters={restaurantVenue.dedupedVotes}
                  canCome={restaurantVenue.canCome}
                  cannotCome={restaurantVenue.cannotCome}
                />
              </QuestionRender>
            )}
          </section>
        )}

        <div className="d-flex flex-wrap gap-2">
          {can_reschedule && (
            <Button
              type="button"
              variant="outline-primary"
              onClick={() => on_open_reschedule(poll_id, current_pub_id, restaurant_id, restaurant_time)}
            >
              Reschedule Event
            </Button>
          )}

          {can_delete_event && (
            <QuestionRender className={styles.actionBlock} question="Delete This Event">
              <ConfirmModal
                title="Delete Current event"
                detail="The current event will be deleted"
                confirm_text="Do Nothing"
                cancel_text="Delete it"
                on_cancel={async () => {
                  try {
                    await deletePoll(poll_id);
                  } catch (error) {
                    notifyError(getUserFacingErrorMessage(error, "Unable to delete this event."));
                  }
                }}
              />
            </QuestionRender>
          )}
        </div>
      </div>
    </article>
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
    <section className="py-4 py-md-5">
      <div className="container">
        <div className="mb-4">
          <h1 className="h2 mb-1">Current Events</h1>
          <p className="text-body-secondary mb-0">Upcoming pub night details and attendance tracking.</p>
        </div>
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
    </section>
  );
}

export default CurrentEvents;
