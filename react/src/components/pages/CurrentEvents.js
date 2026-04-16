// @ts-check

import { useState, useCallback, useEffect } from "react";
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
import Button from "../UI/Button";
import ReschedulePollModal from "./ReschedulePollModal";
import { deletePoll } from "../../dbtools/polls";
import { getUserFacingErrorMessage } from "../../permissions";

/**
 * Ensure a user-supplied URL is absolute so browsers don't treat it as relative.
 * Prepends https:// if no protocol is present.
 * @param {string | undefined} url
 * @returns {string | undefined}
 */
function ensureAbsoluteUrl(url) {
  if (!url) return undefined;
  if (/^https?:\/\//i.test(url)) return url;
  return `https://${url}`;
}
import { notifyError } from "../../utils/notify";
import { buildCurrentEventViewModel } from "../../utils/currentEventViewModel";

/** @typedef {{ name?: string, web_site?: string, pubImage?: string, address?: string }} PubParametersEntry */
/** @typedef {Record<string, PubParametersEntry | undefined>} PubParametersMap */
/** @typedef {{ selected?: string, restaurant?: string | null, restaurant_time?: string | null, date?: string }} EventPollValue */
/** @typedef {{ sortedByDate: (reverse?: boolean) => Iterable<[string, EventPollValue]> }} SortedPollCollection */

/** @typedef {import("../../store").RootState} RootState */

/**
 * @param {unknown} value
 * @returns {string | null}
 */
function normalizeImageUrl(value) {
  if (typeof value !== "string") {
    return null;
  }

  const trimmed = value.trim();
  if (!trimmed) {
    return null;
  }

  if (
    trimmed.startsWith("http://") ||
    trimmed.startsWith("https://") ||
    trimmed.startsWith("//") ||
    trimmed.startsWith("data:image/") ||
    trimmed.startsWith("blob:")
  ) {
    if (
      trimmed.startsWith("http://") &&
      typeof window !== "undefined" &&
      window.location.protocol === "https:"
    ) {
      return `https://${trimmed.slice("http://".length)}`;
    }

    return trimmed;
  }

  return null;
}

/**
 * @param {{ value: EventPollValue, pub_parameters: PubParametersMap }} props
 */
function PastEvent({ value, pub_parameters }) {
  if (!pub_parameters[value.selected]) {
    return <div></div>;
  }

  const pubName = pub_parameters[value.selected].name;
  const pubWebsite = pub_parameters[value.selected]?.web_site;
  const pubImage = normalizeImageUrl(pub_parameters[value.selected]?.pubImage);
  const [hasImageLoadError, setHasImageLoadError] = useState(false);

  useEffect(() => {
    setHasImageLoadError(false);
  }, [pubImage]);

  const shouldShowImage = pubImage && !hasImageLoadError;

  return (
    <div className="col">
      <div className="card shadow-sm h-100">
        {shouldShowImage ? (
          <img
            src={pubImage}
            alt={`Photo of ${pubName}`}
            className={`card-img-top ${styles.pastEventImage}`}
            onError={() => setHasImageLoadError(true)}
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
                href={ensureAbsoluteUrl(pubWebsite)}
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
  const [searchParams, setSearchParams] = useSearchParams();
  const pubCountParam = Number(searchParams.get("pastPageSize"));
  const pubCount = [5, 10, 20].includes(pubCountParam) ? pubCountParam : 5;
  const cursorTrail = (searchParams.get("pastCursorTrail") || "")
    .split(",")
    .filter(Boolean);
  const currentCursorId = cursorTrail[cursorTrail.length - 1] || null;

  const { pollData, hasNextPage, lastVisibleId, isLoading } = usePastCompletePolls(
    pubCount,
    currentCursorId
  );

  const hasPreviousPage = cursorTrail.length > 0;
  const pageIndex = cursorTrail.length;
  const pub_parameters = usePubs();
  const sortedPollsByDate = [...pollData.sortedByDate(true)];

  const writePaginationParams = useCallback(
    (nextPageSize, nextCursorTrail) => {
      const nextParams = new URLSearchParams(searchParams);
      nextParams.set("pastPageSize", String(nextPageSize));
      if (nextCursorTrail.length > 0) {
        nextParams.set("pastCursorTrail", nextCursorTrail.join(","));
      } else {
        nextParams.delete("pastCursorTrail");
      }
      setSearchParams(nextParams);
    },
    [searchParams, setSearchParams]
  );

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
              value={pubCount}
              onChange={selectPubCountHandler}
            >
              <option>5</option>
              <option>10</option>
              <option>20</option>
            </select>
          </div>
        </div>

        <div className="row row-cols-1 row-cols-sm-2 row-cols-lg-3 g-4">
          {sortedPollsByDate.map(([key, value]) => {
            return <PastEvent key={key} value={value} pub_parameters={pub_parameters} />;
          })}
        </div>

        <div className="d-flex align-items-center justify-content-between mt-4 gap-3 flex-wrap">
          <div className="text-body-secondary">Page {pageIndex + 1}</div>
          <div className="d-flex gap-2">
            <button
              type="button"
              className="btn btn-outline-secondary"
              onClick={goToPreviousPage}
              disabled={!hasPreviousPage || isLoading}
            >
              Newer Events
            </button>
            <button
              type="button"
              className="btn btn-outline-secondary"
              onClick={goToNextPage}
              disabled={!hasNextPage || isLoading}
            >
              Older Events
            </button>
          </div>
        </div>
      </div>
    </section>
  );
}

/**
 * @param {{
 *  poll_id: string,
 *  current_pub_id: string,
 *  restaurant_id: string | null | undefined,
 *  restaurant_time: string | null | undefined,
 *  date: string,
 *  pub_parameters: PubParametersMap,
 *  can_reschedule: boolean,
 *  can_delete_event: boolean,
 *  show_voters: boolean,
 *  on_open_reschedule: (pollId: string, pubId: string, restaurantId: string | null | undefined, restaurantTime: string | null | undefined) => void,
 * }} props
 */
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
  const currUserId = useSelector(
    /** @param {RootState} state */
    (state) => state.auth.uid
  );
  const normalizedUserId = typeof currUserId === "string" ? currUserId : null;
  const canReadProtectedEventData = Boolean(normalizedUserId);
  const [votes] = useVotes(poll_id, canReadProtectedEventData);
  const [attendance, setAttendanceStatus, clearAttendance] = useAttendance(
    poll_id,
    canReadProtectedEventData
  );
  const eventViewModel = buildCurrentEventViewModel({
    current_pub_id,
    restaurant_id,
    restaurant_time,
    pub_parameters,
    votes,
    attendance,
    currUserId: normalizedUserId,
    show_voters,
  });

  if (!eventViewModel) {
    return <div></div>;
  }
  const { mainVenue, restaurantVenue } = eventViewModel;
  const mainVenueImage = normalizeImageUrl(mainVenue.image);
  const [hasMainImageLoadError, setHasMainImageLoadError] = useState(false);

  useEffect(() => {
    setHasMainImageLoadError(false);
  }, [mainVenueImage]);

  const shouldShowMainImage = mainVenueImage && !hasMainImageLoadError;

  const attendanceHandlers = useEventAttendance(
    normalizedUserId,
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
                <a
                  href={ensureAbsoluteUrl(mainVenue.website)}
                  target="_blank"
                  rel="noreferrer"
                  className="link-primary text-decoration-none"
                >
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

        {shouldShowMainImage && (
          <div className="mb-3">
            <img
              src={mainVenueImage}
              alt="What the venue looks like"
              className={`img-fluid rounded ${styles.image}`}
              loading="lazy"
              decoding="async"
              onError={() => setHasMainImageLoadError(true)}
            />
          </div>
        )}

        {!shouldShowMainImage && (
          <div className={`mb-3 p-3 rounded ${styles.currentEventImagePlaceholder}`}>
            <span className="text-body-secondary">No image available</span>
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
              onClick={() =>
                on_open_reschedule(poll_id, current_pub_id, restaurant_id, restaurant_time)
              }
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
          <p className="text-body-secondary mb-0">
            Upcoming pub night details and attendance tracking.
          </p>
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
