// @ts-check

import { useState, useCallback, useEffect } from "react";
import { useSelector } from "react-redux";
import { NavLink, useSearchParams } from "react-router-dom";
import { usePastMeets, useCurrentMeets } from "../../hooks/useMeets";
import useVenues from "../../hooks/useVenues";
import useVotes from "../../hooks/useVotes";
import useAttendance from "../../hooks/useAttendance";
import useRole from "../../hooks/useRole";
import useAdmin from "../../hooks/useAdmin";
import useUserPrivateData from "../../hooks/useUserPrivateData";
import useOnlineStatus from "../../hooks/useOnlineStatus";
import { useRescheduleMeet } from "../../hooks/useRescheduleMeet";
import { useMeetAttendance } from "../../hooks/useMeetAttendance";
import styles from "./CurrentMeets.module.css";
import ShowAttendance from "../UI/ShowAttendance";
import AttendanceActions from "../UI/AttendanceActions";
import ConfirmModal, { QuestionRender } from "../UI/ConfirmModal";
import Button from "../UI/Button";
import RescheduleMeetModal from "./RescheduleMeetModal";
import { deleteMeet } from "../../dbtools/polls";
import { getUserFacingErrorMessage } from "../../permissions";
import MeetChatModal from "./MeetChatModal";
import ETAInput from "../UI/ETAInput";
import { normalizeArrivalTime } from "../../utils/arrivalTime";

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
import { buildCurrentMeetViewModel } from "../../utils/currentMeetViewModel";

/** @typedef {{ name?: string, website?: string, image?: string, address?: string }} VenueEntry */
/** @typedef {Record<string, VenueEntry | undefined>} VenueMap */
/** @typedef {{ id?: string, status?: string, name?: string, address?: string, image_url?: string | null }} SelectedVenueSummary */
/** @typedef {{ selectedVenueId?: string, selectedVenueSummary?: SelectedVenueSummary | null, restaurantId?: string | null, restaurantTime?: string | null, date?: string }} MeetValue */
/** @typedef {{ sortedByDate: (reverse?: boolean) => Iterable<[string, MeetValue]> }} SortedMeetCollection */

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
 * @param {{ value: MeetValue, venues: VenueMap, meetId: string, canChat: boolean }} props
 */
function PastMeet({ value, venues, meetId, canChat }) {
  const currUserId = useSelector(
    /** @param {RootState} state */
    (state) => {
      const uid = state.auth?.uid;
      return typeof uid === "string" && uid.length > 0 ? uid : null;
    }
  );
  const [attendance, , , , , , clearEta] = useAttendance(meetId, Boolean(currUserId));
  const [hasImageLoadError, setHasImageLoadError] = useState(false);
  const [isChatOpen, setIsChatOpen] = useState(false);

  const venue = venues[value.selectedVenueId];
  const venueName = venue?.name;
  const venueWebsite = venue?.website;
  const venueImage = normalizeImageUrl(venue?.image);
  const userEta = (currUserId && venue && attendance[value.selectedVenueId]?.eta?.[currUserId]) || null;

  useEffect(() => {
    setHasImageLoadError(false);
  }, [venueImage]);

  if (!venue) {
    return <div></div>;
  }

  const shouldShowImage = venueImage && !hasImageLoadError;

  return (
    <div className="col">
      <div className="card shadow-sm h-100">
        {shouldShowImage ? (
          <img
            src={venueImage}
            alt={`Photo of ${venueName}`}
            className={`card-img-top ${styles.pastMeetImage}`}
            onError={() => setHasImageLoadError(true)}
          />
        ) : (
          <div
            className={`${styles.pastMeetImagePlaceholder} card-img-top d-flex align-items-center justify-content-center`}
          >
            <span className="text-body-secondary">No image available</span>
          </div>
        )}
        <div className="card-body d-flex flex-column">
          <h5 className="card-title mb-2">
            {venueWebsite ? (
              <a
                href={ensureAbsoluteUrl(venueWebsite)}
                target="_blank"
                rel="noreferrer"
              >
                {venueName}
              </a>
            ) : (
              venueName
            )}
          </h5>
          {userEta && currUserId && (
            <div className="d-flex align-items-center gap-2 mb-2">
              <span className="text-body-secondary small">Your ETA: <strong>{userEta}</strong></span>
              <Button
                type="button"
                variant="outline-danger"
                className="btn-sm"
                onClick={() => clearEta(value.selectedVenueId, currUserId)}
              >
                Remove ETA
              </Button>
            </div>
          )}
          <div className="d-flex align-items-center justify-content-between mt-auto pt-2">
            <p className="card-text text-body-secondary mb-0">{value.date}</p>
            {canChat && (
              <Button
                type="button"
                variant="outline-secondary"
                className="btn-sm"
                onClick={() => setIsChatOpen(true)}
              >
                Meet Chat
              </Button>
            )}
          </div>
        </div>
      </div>

      {isChatOpen && (
        <MeetChatModal meetId={meetId} onClose={() => setIsChatOpen(false)} />
      )}
    </div>
  );
}

export function PastMeets() {
  const [searchParams, setSearchParams] = useSearchParams();
  const pageSizeParam = Number(searchParams.get("pastPageSize"));
  const pageSize = [5, 10, 20].includes(pageSizeParam) ? pageSizeParam : 5;
  const cursorTrail = (searchParams.get("pastCursorTrail") || "")
    .split(",")
    .filter(Boolean);
  const currentCursorId = cursorTrail[cursorTrail.length - 1] || null;

  const { meetData, hasNextPage, lastVisibleId, isLoading } = usePastMeets(
    pageSize,
    currentCursorId
  );

  const hasPreviousPage = cursorTrail.length > 0;
  const pageIndex = cursorTrail.length;
  const venues = useVenues();
  const sortedMeetsByDate = [...meetData.sortedByDate(true)];
  const canChat = useRole("canChat");
  const canViewStats = useAdmin();

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

  const selectPageSizeHandler = useCallback(
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
    writePaginationParams(pageSize, [...cursorTrail, lastVisibleId]);
  }, [hasNextPage, lastVisibleId, isLoading, writePaginationParams, pageSize, cursorTrail]);

  const goToPreviousPage = useCallback(() => {
    if (!hasPreviousPage || isLoading) {
      return;
    }
    writePaginationParams(pageSize, cursorTrail.slice(0, -1));
  }, [hasPreviousPage, isLoading, writePaginationParams, pageSize, cursorTrail]);

  return (
    <section className="py-4 py-md-5">
      <div className="container">
        <div className="row mb-4 align-items-end g-3">
          <div className="col-md-8">
            <h1 className="h2 mb-2">Past Meets</h1>
            <p className="text-body-secondary mb-0">
              Browse previous Meets in an album-style view.
            </p>
          </div>
          <div className="col-md-4">
            <label htmlFor="pastMeetCount" className="form-label">
              Number of past meets to show
            </label>
            <select
              id="pastMeetCount"
              className="form-select"
              value={pageSize}
              onChange={selectPageSizeHandler}
            >
              <option>5</option>
              <option>10</option>
              <option>20</option>
            </select>
            {canViewStats && (
              <div className="mt-2 d-grid gap-2">
                <NavLink className="btn btn-outline-secondary" to="/stats/winning_venues">
                  View winning venue stats
                </NavLink>
                <NavLink className="btn btn-outline-secondary" to="/stats/attendance">
                  View attendance stats
                </NavLink>
              </div>
            )}
          </div>
        </div>

        <div className="row row-cols-1 row-cols-sm-2 row-cols-lg-3 g-4">
          {sortedMeetsByDate.map(([key, value]) => {
            return <PastMeet key={key} meetId={key} value={value} venues={venues} canChat={canChat} />;
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
              Newer Meets
            </button>
            <button
              type="button"
              className="btn btn-outline-secondary"
              onClick={goToNextPage}
              disabled={!hasNextPage || isLoading}
            >
              Older Meets
            </button>
          </div>
        </div>
      </div>
    </section>
  );
}

/**
 * @param {{
 *  meetId: string,
 *  currentVenueId: string,
 *  selectedVenueSummary?: SelectedVenueSummary | null,
 *  restaurantId: string | null | undefined,
 *  restaurantTime: string | null | undefined,
 *  date: string,
 *  venues: VenueMap,
 *  canReschedule: boolean,
 *  canDeleteMeet: boolean,
 *  canChat: boolean,
 *  etaDefaultTime: string,
 *  showVoters: boolean,
 *  onOpenReschedule: (meetId: string, venueId: string, restaurantId: string | null | undefined, restaurantTime: string | null | undefined) => void,
 * }} props
 */
function CurrentMeet({
  meetId,
  currentVenueId,
  selectedVenueSummary,
  restaurantId,
  restaurantTime,
  date,
  venues,
  canReschedule,
  canDeleteMeet,
  canChat,
  etaDefaultTime,
  showVoters,
  onOpenReschedule,
}) {
  const currUserId = useSelector(
    /** @param {RootState} state */
    (state) => state.auth.uid
  );
  const normalizedUserId = typeof currUserId === "string" ? currUserId : null;
  const canReadProtectedMeetData = Boolean(normalizedUserId);
  const [isChatOpen, setIsChatOpen] = useState(false);
  const { isOnline } = useOnlineStatus();
  const [votes] = useVotes(meetId, canReadProtectedMeetData);
  const [attendance, setAttendanceStatus, clearAttendance, , , setEta, clearEta] = useAttendance(
    meetId,
    canReadProtectedMeetData
  );
  let currentMeetVenues = venues;
  if (selectedVenueSummary?.status === "active" || selectedVenueSummary?.status === "deleted") {
    currentMeetVenues = {
      ...venues,
      [currentVenueId]: {
        name: selectedVenueSummary.name,
        address: selectedVenueSummary.address,
        image: selectedVenueSummary.image_url || undefined,
      },
    };
  } else if (selectedVenueSummary?.status === "missing") {
    currentMeetVenues = {
      ...venues,
      [currentVenueId]: { name: "Venue details unavailable" },
    };
  }
  const meetViewModel = buildCurrentMeetViewModel({
    currentVenueId,
    restaurantId,
    restaurantTime,
    venues: currentMeetVenues,
    votes,
    attendance,
    currUserId: normalizedUserId,
    showVoters,
  });

  const mainVenueImage = normalizeImageUrl(meetViewModel?.mainVenue?.image);
  const [hasMainImageLoadError, setHasMainImageLoadError] = useState(false);

  useEffect(() => {
    setHasMainImageLoadError(false);
  }, [mainVenueImage]);

  const attendanceHandlers = useMeetAttendance(
    normalizedUserId,
    setAttendanceStatus,
    clearAttendance,
    meetViewModel?.mainVenue?.id ?? "",
    meetViewModel?.restaurantVenue?.id,
    setEta,
    clearEta
  );

  if (!meetViewModel) {
    return <div></div>;
  }
  const { mainVenue, restaurantVenue } = meetViewModel;
  const shouldShowMainImage = mainVenueImage && !hasMainImageLoadError;

  return (
    <>
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
              {selectedVenueSummary?.status === "deleted" && (
                <span className="badge text-bg-secondary ms-2">Deleted</span>
              )}
              <p className="text-body-secondary mb-0">{date}</p>
            </div>
            {canChat && (
              <Button
                type="button"
                variant="outline-secondary"
                className="btn-sm flex-shrink-0"
                onClick={() => setIsChatOpen(true)}
              >
                Meet Chat
              </Button>
            )}
          </div>

          {mainVenue.address && <p className="mb-3">{mainVenue.address}</p>}
          {selectedVenueSummary?.status === "missing" && (
            <p className="text-body-secondary mb-3">This Meet references a Venue that is no longer available.</p>
          )}

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
            <div className={`mb-3 p-3 rounded ${styles.currentMeetImagePlaceholder}`}>
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

          {currUserId && mainVenue.userCanCome && (
            <ETAInput
              className="mb-3"
              userEta={mainVenue.userEta}
              defaultEta={etaDefaultTime}
              onSetEta={attendanceHandlers.setMainEta}
              onClearEta={attendanceHandlers.clearMainEta}
            />
          )}

          {mainVenue.allowShowVoters && (
            <div className="mb-3">
              <QuestionRender className={styles.actionBlock} question="Show venue attendance">
                <ShowAttendance
                  voters={mainVenue.dedupedVotes}
                  canCome={mainVenue.canCome}
                  cannotCome={mainVenue.cannotCome}
                  eta={mainVenue.etaMap}
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

              {currUserId && restaurantVenue.userCanCome && (
                <ETAInput
                  className="mb-3"
                  userEta={restaurantVenue.userEta}
                  defaultEta={etaDefaultTime}
                  onSetEta={attendanceHandlers.setRestaurantEta}
                  onClearEta={attendanceHandlers.clearRestaurantEta}
                />
              )}

              {restaurantVenue.allowShowVoters && (
                <QuestionRender className={styles.actionBlock} question="Show restaurant attendance">
                  <ShowAttendance
                    voters={restaurantVenue.dedupedVotes}
                    canCome={restaurantVenue.canCome}
                    cannotCome={restaurantVenue.cannotCome}
                    eta={restaurantVenue.etaMap}
                  />
                </QuestionRender>
              )}
            </section>
          )}

          <div className="d-flex flex-wrap gap-2">
            {canReschedule && (
              <Button
                type="button"
                variant="outline-primary"
                disabled={!isOnline}
                title={isOnline ? undefined : "Unavailable offline"}
                onClick={() =>
                  onOpenReschedule(meetId, currentVenueId, restaurantId, restaurantTime)
                }
              >
                Reschedule Meet
              </Button>
            )}


            {canDeleteMeet && (
              isOnline ? (
                <QuestionRender className={styles.actionBlock} question="Delete This Meet">
                  <ConfirmModal
                    title="Delete Current Meet"
                    detail="The current meet will be deleted"
                    confirm_text="Do Nothing"
                    cancel_text="Delete it"
                    on_cancel={async () => {
                      try {
                        await deleteMeet(meetId);
                      } catch (error) {
                        notifyError(getUserFacingErrorMessage(error, "Unable to delete this meet."));
                      }
                    }}
                  />
                </QuestionRender>
              ) : (
                <Button type="button" variant="outline-danger" disabled title="Unavailable offline">
                  Delete This Meet
                </Button>
              )
            )}
          </div>
        </div>
      </article>

      {isChatOpen && (
        <MeetChatModal meetId={meetId} onClose={() => setIsChatOpen(false)} />
      )}
    </>
  );
}

function CurrentMeets() {
  const currUserId = useSelector(
    /** @param {RootState} state */
    (state) => {
      const uid = state.auth?.uid;
      return typeof uid === "string" && uid.length > 0 ? uid : null;
    }
  );
  const groupId = useSelector(
    /** @param {RootState} state */
    (state) => state.group?.groupId ?? null
  );
  const privateData = useUserPrivateData(currUserId);
  const etaDefaultTime = normalizeArrivalTime(privateData?.defaultArrivalTime);
  const currentMeetsState = useCurrentMeets(groupId, currUserId);
  const meetData = currentMeetsState.meetData ?? currentMeetsState;
  const currentMeetEntries = [...meetData.sortedByDate()];
  const venues = useVenues();
  const canReschedule = useRole("canCompletePoll");
  const canDeleteMeet = useRole("canCreatePoll");
  const canShowVoters = useRole("canShowVoters");
  const canChat = useRole("canChat");
  const rescheduleState = useRescheduleMeet(venues, canReschedule);

  return (
    <section className="py-4 py-md-5">
      <div className="container">
        <div className="mb-4">
          <h1 className="h2 mb-1">Current Meets</h1>
          <p className="text-body-secondary mb-0">
            Upcoming Meet details and attendance tracking.
          </p>
        </div>
        {currentMeetsState.isLoading && <p role="status">Loading Current Meets...</p>}
        {currentMeetsState.errorMessage && (
          <div className="alert alert-warning" role="alert">
            <p className="mb-2">{currentMeetsState.errorMessage}</p>
            {currentMeetsState.isStale && <p className="mb-2">Showing the last received snapshot.</p>}
            <button type="button" className="btn btn-outline-secondary btn-sm" onClick={currentMeetsState.retry}>
              Retry
            </button>
          </div>
        )}
        {!currentMeetsState.isLoading && !currentMeetsState.errorMessage && currentMeetEntries.length === 0 && (
          <p className="text-body-secondary">There are no current Meets for this Group.</p>
        )}
        {rescheduleState.isRescheduling && (
          <RescheduleMeetModal
            selectedVenueId={rescheduleState.selectedVenueId}
            selectedVenueHasFood={rescheduleState.selectedVenueHasFood}
            mainVenueOptions={rescheduleState.mainVenueOptions}
            restaurantOptions={rescheduleState.restaurantOptions}
            chosenRestaurantId={rescheduleState.chosenRestaurantId}
            restaurantTime={rescheduleState.restaurantTime}
            onVenueChange={rescheduleState.setSelectedVenue}
            onRestaurantChange={rescheduleState.setRestaurantChoice}
            onRestaurantTimeChange={rescheduleState.setRestaurantTime}
            onConfirm={rescheduleState.saveReschedule}
            onCancel={rescheduleState.cancelReschedule}
          />
        )}
        {currentMeetEntries.map(([key, value]) => {
          return (
            <CurrentMeet
              key={key}
              meetId={key}
              currentVenueId={value.selectedVenueId}
              selectedVenueSummary={value.selectedVenueSummary}
              restaurantId={value.restaurantId}
              restaurantTime={value.restaurantTime}
              date={value.date}
              venues={venues}
              canReschedule={canReschedule}
              canDeleteMeet={canDeleteMeet}
              canChat={canChat}
              etaDefaultTime={etaDefaultTime}
              showVoters={canShowVoters}
              onOpenReschedule={rescheduleState.openRescheduleModal}
            />
          );
        })}
      </div>
    </section>
  );
}

export default CurrentMeets;
