// @ts-check

import { useSelector } from "react-redux";
import styles from "./PollVote.module.css";
import useVotes from "../../hooks/useVotes";
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import useAttendance from "../../hooks/useAttendance";
import useRole from "../../hooks/useRole";
import { useVotableRow } from "../../hooks/useVotableRow";
import { usePollRows } from "../../hooks/usePollRows";
import { useBallotActions } from "../../hooks/useBallotActions";
import useUserPrivateData from "../../hooks/useUserPrivateData";
import useUsers from "../../hooks/useUsers";
import ShowAttendance from "./ShowAttendance";
import AttendanceActions from "./AttendanceActions";
import ETAInput from "./ETAInput";
import { QuestionRender } from "./ConfirmModal";
import Button from "./Button";
import { normalizeArrivalTime } from "../../utils/arrivalTime";

/** @type {Record<string, { uid: string, name?: string, photoUrl?: string }>} */
const EMPTY_ATTENDANCE_PROFILES = {};

/** @typedef {import("../../store").RootState} RootState */

/** @typedef {{ total: number, anonymousCount: number, voterUids: string[] }} VotingOption */
/** @typedef {Record<string, VotingOption | undefined>} VotesMap */
/** @typedef {Record<string, { canCome?: string[], cannotCome?: string[], eta?: Record<string, string> } | undefined>} AttendanceMap */
/** @typedef {{ name?: string, venueType?: string }} PollVenueEntry */
/** @typedef {{ venues?: Record<string, PollVenueEntry | boolean>, date?: string }} PollData */
/** @typedef {{ uid?: string, name?: string, votesVisible?: boolean }} UserEntry */

/**
 * @typedef {Object} RespondMenuProps
 * @property {boolean} votedFor
 * @property {boolean} canVote
 * @property {boolean} votePending
 * @property {boolean | undefined} retrySelection
 * @property {boolean} userCanCome
 * @property {boolean} userCannotCome
 * @property {string | undefined} userEta
 * @property {boolean} allowAttendanceControls
 * @property {boolean} allowGlobalAttendanceControls
 * @property {() => Promise<boolean>} onVote
 * @property {() => Promise<boolean>} onRetryVote
 * @property {(status: "canCome" | "cannotCome") => Promise<void>} onSetAttendanceStatus
 * @property {() => Promise<void>} onClearAttendance
 * @property {(eta: string) => Promise<void>} onSetEta
 * @property {() => Promise<void>} onClearEta
 * @property {string} defaultEta
 * @property {() => Promise<void>} onSetAllCanCome
 * @property {() => Promise<void>} onSetAllCannotCome
 */

/**
 * @typedef {Object} VotableVenueProps
 * @property {string} venueId
 * @property {string} venueName
 * @property {string} venueType
 * @property {string | null | undefined} currUserId
 * @property {VotesMap} votes
 * @property {string[]} selectedOptionIds
 * @property {string[]} pendingOptionIds
 * @property {Record<string, boolean>} retrySelections
 * @property {Record<string, UserEntry>} voterProfiles
 * @property {Record<string, { uid: string, name?: string, photoUrl?: string }>} attendanceProfiles
 * @property {boolean} canCastVote
 * @property {AttendanceMap} attendance
 * @property {boolean} canShowAttendance
 * @property {boolean} canShowVoters
 * @property {(venueId: string, selected: boolean) => Promise<boolean>} changeVote
 * @property {(venueId: string) => Promise<boolean>} retryVote
 * @property {(venueId: string, userId: string, status: "canCome" | "cannotCome") => Promise<void>} setAttendanceStatus
 * @property {(venueId: string, userId: string) => Promise<void>} clearAttendance
 * @property {(venueId: string, userId: string, eta: string) => Promise<void>} setEta
 * @property {(venueId: string, userId: string) => Promise<void>} clearEta
 * @property {string} defaultEta
 * @property {() => Promise<void>} setAllAttendanceToCanCome
 * @property {() => Promise<void>} setAllAttendanceToCannotCome
 * @property {string[]} pollVenueIds
 * @property {boolean} showDeleteColumn
 * @property {boolean} allowDelete
 * @property {boolean} allowCompletePoll
 * @property {string} pollId
 * @property {string | undefined} pollDate
 * @property {() => void} completeHandler
 * @property {boolean} mobile
 * @property {boolean} showAttendanceColumns
 * @property {Record<string, UserEntry>} usersByUid
 */

/**
 * @typedef {Object} PollVoteProps
 * @property {string} poll_id
 * @property {PollData} poll_data
 * @property {Record<string, PollVenueEntry | undefined>=} venues
 * @property {(venueId: string, venueName: string, pollId: string) => void} on_complete
 * @property {boolean=} mobile
 */

/**
 * @param {unknown} value
 * @returns {string}
 */
function normalizeUserId(value) {
  if (value === null || value === undefined) {
    return "";
  }
  return String(value).trim();
}

/**
 * @param {string[]} ids
 * @param {Record<string, UserEntry>} usersByUid
 * @param {Record<string, string>=} etaMap
 * @returns {string[]}
 */
function formatPeople(ids, usersByUid, etaMap = {}) {
  return [...new Set(ids.map(normalizeUserId).filter(Boolean))]
    .map((id) => {
      const displayName = usersByUid[id]?.name || "No Name Recorded";
      const eta = etaMap[id];
      return eta ? `${displayName} (${eta})` : displayName;
    })
    .sort((a, b) => a.localeCompare(b));
}

/** @param {RespondMenuProps} props */
function RespondMenu({
  votedFor,
  canVote,
  votePending,
  retrySelection,
  userCanCome,
  userCannotCome,
  userEta,
  allowAttendanceControls,
  allowGlobalAttendanceControls,
  onVote,
  onRetryVote,
  onSetAttendanceStatus,
  onClearAttendance,
  onSetEta,
  onClearEta,
  defaultEta,
  onSetAllCanCome,
  onSetAllCannotCome,
}) {
  const [showActions, setShowActions] = useState(false);
  /** @type {[import("react").CSSProperties | null, import("react").Dispatch<import("react").SetStateAction<import("react").CSSProperties | null>>]} */
  const [panelStyle, setPanelStyle] = useState(null);
  /** @type {import("react").RefObject<HTMLDivElement | null>} */
  const menuRef = useRef(null);
  /** @type {import("react").RefObject<HTMLButtonElement | null>} */
  const triggerRef = useRef(null);
  /** @type {import("react").RefObject<HTMLDivElement | null>} */
  const panelRef = useRef(null);

  useLayoutEffect(() => {
    if (!showActions || !panelRef.current || !triggerRef.current) {
      return;
    }

    const gutter = 8;

    const updatePanelPosition = () => {
      if (!panelRef.current || !triggerRef.current) {
        return;
      }

      const triggerRect = triggerRef.current.getBoundingClientRect();
      const panelRect = panelRef.current.getBoundingClientRect();
      const spaceAbove = triggerRect.top;
      const spaceBelow = window.innerHeight - triggerRect.bottom;
      const spaceRight = window.innerWidth - triggerRect.right;

      let top = triggerRect.bottom + gutter;
      let left = triggerRect.left;

      if (spaceBelow >= panelRect.height + gutter) {
        // initial value of `top` already equals triggerRect.bottom + gutter
      } else if (spaceAbove >= panelRect.height + gutter) {
        top = triggerRect.top - panelRect.height - gutter;
      } else if (spaceRight >= panelRect.width + gutter) {
        top = triggerRect.top;
        left = triggerRect.right + gutter;
      } else {
        top = Math.max(gutter, window.innerHeight - panelRect.height - gutter);
      }

      left = Math.max(gutter, Math.min(left, window.innerWidth - panelRect.width - gutter));
      top = Math.max(gutter, Math.min(top, window.innerHeight - panelRect.height - gutter));

      setPanelStyle({
        top: `${top}px`,
        left: `${left}px`,
      });
    };

    updatePanelPosition();
    window.addEventListener("resize", updatePanelPosition);
    window.addEventListener("scroll", updatePanelPosition, true);

    return () => {
      window.removeEventListener("resize", updatePanelPosition);
      window.removeEventListener("scroll", updatePanelPosition, true);
    };
  }, [showActions, allowAttendanceControls, allowGlobalAttendanceControls]);

  useEffect(() => {
    if (!showActions) {
      setPanelStyle(null);
    }
  }, [showActions]);

  useEffect(() => {
    if (!showActions) {
      return;
    }

    /** @param {MouseEvent} event */
    const closeIfOutside = (event) => {
      if (menuRef.current && event.target instanceof Node && !menuRef.current.contains(event.target)) {
        setShowActions(false);
      }
    };

    /** @param {KeyboardEvent} event */
    const closeOnEscape = (event) => {
      if (event.key === "Escape") {
        setShowActions(false);
      }
    };

    document.addEventListener("mousedown", closeIfOutside);
    document.addEventListener("keydown", closeOnEscape);
    return () => {
      document.removeEventListener("mousedown", closeIfOutside);
      document.removeEventListener("keydown", closeOnEscape);
    };
  }, [showActions]);

  const toggleActions = useCallback(() => {
    setShowActions((prev) => !prev);
  }, []);

  /** @type {(eta: string) => Promise<void>} */
  const setEtaAndEnsureAttending = useCallback(async (eta) => {
    if (!userCanCome) {
      try {
        await onSetAttendanceStatus("canCome");
      } catch {
        return;
      }
    }
    await onSetEta(eta);
  }, [userCanCome, onSetAttendanceStatus, onSetEta]);

  return (
    <div className={styles.respondMenu} ref={menuRef}>
      <Button
        ref={triggerRef}
        type="button"
        variant="success"
        className={`${styles.respondSummary} ${showActions ? styles.respondSummaryActive : ""}`}
        onClick={toggleActions}
        aria-haspopup="menu"
        aria-expanded={showActions}
      >
        Respond
      </Button>
      {showActions && (
        <div
          ref={panelRef}
          className={styles.respondPanel}
          style={panelStyle ?? undefined}
        >
          {canVote && <Button
            type="button"
            variant={votedFor ? "warning" : "success"}
            className={`${styles.voteButton} ${votedFor ? styles.voted : ""}`}
            disabled={votePending}
            aria-busy={votePending}
            onClick={async () => {
              if (await onVote()) setShowActions(false);
            }}
            title={votedFor ? "Cancel your vote" : "Vote for this venue"}
          >
            {votePending ? "Saving..." : votedFor ? "Cancel Vote" : "Vote Up"}
          </Button>}
          {canVote && retrySelection !== undefined && (
            <Button
              type="button"
              variant="warning"
              className={styles.voteButton}
              onClick={async () => {
                if (await onRetryVote()) setShowActions(false);
              }}
              title="Retry the unconfirmed vote change"
            >
              {retrySelection ? "Retry Vote" : "Retry Withdrawal"}
            </Button>
          )}
          {allowAttendanceControls && (
            <AttendanceActions
              canComeSelected={userCanCome}
              cannotComeSelected={userCannotCome}
              canComeVariant="success"
              cannotComeVariant="danger"
              buttonClassName={styles.attendanceButton}
              canComeSelectedClassName={styles.attending}
              cannotComeSelectedClassName={styles.notAttending}
              canComeLabel="Can come"
              canComeSelectedLabel="Cancel attendance"
              cannotComeLabel="Cannot come"
              cannotComeSelectedLabel="Cancel attendance"
              canComeTitle="Mark that you can come"
              cannotComeTitle="Mark that you cannot come"
              onSetStatus={onSetAttendanceStatus}
              onClear={onClearAttendance}
              onAfterAction={() => setShowActions(false)}
            />
          )}
          {allowAttendanceControls && (
            <ETAInput
              userEta={userEta}
              onSetEta={setEtaAndEnsureAttending}
              onClearEta={onClearEta}
              defaultEta={defaultEta}
              className={styles.etaInput}
              addButtonVariant="warning"
              addButtonClassName={styles.attendanceButton}
            />
          )}
          {allowGlobalAttendanceControls && (
            <>
              <div className={styles.panelDivider}></div>
              <Button
                type="button"
                variant="success"
                className={styles.comingAllButton}
                onClick={async () => {
                  await onSetAllCanCome();
                  setShowActions(false);
                }}
                title="Set your standing response for any main Venue"
              >
                I'm coming (any venue)
              </Button>
              <Button
                type="button"
                variant="danger"
                className={styles.cannotComeAllButton}
                onClick={async () => {
                  await onSetAllCannotCome();
                  setShowActions(false);
                }}
                title="Set your standing response for any main Venue"
              >
                I can't come (any venue)
              </Button>
              {(userCanCome || userCannotCome) && (
                <Button
                  type="button"
                  variant="secondary"
                  className={styles.cannotComeAllButton}
                  onClick={async () => {
                    await onClearAttendance();
                    setShowActions(false);
                  }}
                  title="Withdraw your Any-Venue response"
                >
                  Clear response
                </Button>
              )}
            </>
          )}
        </div>
      )}
    </div>
  );
}

/** @param {VotableVenueProps} props */
function VotableVenue({
  venueId,
  venueName,
  venueType,
  currUserId,
  votes,
  selectedOptionIds,
  pendingOptionIds,
  retrySelections,
  attendance,
  canShowAttendance,
  canShowVoters,
  canCastVote,
  changeVote,
  retryVote,
  setAttendanceStatus,
  clearAttendance,
  setEta,
  clearEta,
  defaultEta,
  setAllAttendanceToCanCome,
  setAllAttendanceToCannotCome,
  pollVenueIds,
  showDeleteColumn,
  allowDelete,
  allowCompletePoll,
  pollId,
  completeHandler,
  showAttendanceColumns,
  usersByUid,
  voterProfiles,
  attendanceProfiles,
}) {
  const rowData = useVotableRow(
    venueId,
    currUserId,
    votes,
    selectedOptionIds,
    pendingOptionIds,
    retrySelections,
    attendance,
    setAttendanceStatus,
    clearAttendance,
    changeVote,
    retryVote,
    setEta,
    clearEta,
    pollId,
    venueType
  );

  const allowGlobalAttendanceControls = rowData.canVote && venueId === "any";
  const [votedNames, canComeNames, cannotComeNames, etaNames] = showAttendanceColumns

    ? (() => {

      const rowAttendance = attendance[venueId] || {};

      const rowEtaMap = rowAttendance.eta || {};

      const rowVoters = canShowVoters
        ? (votes[venueId]?.voterUids || []).filter((id) => voterProfiles[normalizeUserId(id)]?.votesVisible !== false)
        : [];

      const rowCanCome = rowAttendance.canCome || [];

      const rowCannotCome = rowAttendance.cannotCome || [];


      return [

        formatPeople(rowVoters, voterProfiles),

        formatPeople(rowCanCome, usersByUid),

        formatPeople(rowCannotCome, usersByUid),

        formatPeople(

          rowCanCome.filter((id) => Boolean(rowEtaMap[normalizeUserId(id)])),

          usersByUid,

          rowEtaMap

        ),

      ];

    })()

    : [[], [], [], []];


  return (
    <tr>
      {showDeleteColumn && (
        <td className={styles.deleteCol}>
          {allowDelete && (
            <Button
              type="button"
              variant="danger"
              className={styles.deleter}
              onClick={rowData.deleteHandler}
              title="Remove this venue from the poll"
            >
              Delete
            </Button>
          )}
        </td>
      )}
      <td className={styles.venueCol}>
        {allowCompletePoll ? (
          <Button
            type="button"
            variant="secondary"
            className={styles.venueButton}
            onClick={completeHandler}
            title="Select this venue as the winner to complete the poll"
          >
            {venueName}
          </Button>
        ) : (
          <label>{venueName}</label>
        )}
      </td>
      <td className={styles.voteCol}>
        <label>{rowData.voteCount}</label>
      </td>
      {rowData.canVote && (
        <td className={`${styles.statusCol} ${styles.attendanceIndicator}`}>
          {venueId !== "any" && (
            rowData.userCanCome ? <span title="You said you can come">✅</span>
              : rowData.userCannotCome ? <span title="You said you cannot come">❌</span>
                : null
          )}
        </td>
      )}

      {rowData.canVote && (
        <td className={styles.actionsCol}>
          <div className={styles.actionButtons}>
            <RespondMenu
              votedFor={rowData.votedFor}
              canVote={canCastVote}
              votePending={rowData.votePending}
              retrySelection={rowData.retrySelection}
              userCanCome={rowData.userCanCome}
              userCannotCome={rowData.userCannotCome}
              userEta={rowData.userEta}
              allowAttendanceControls={rowData.allowAttendanceControls}
              allowGlobalAttendanceControls={allowGlobalAttendanceControls}
              onVote={rowData.voteHandler}
              onRetryVote={rowData.retryVoteHandler}
              onSetAttendanceStatus={rowData.setAttendanceStatusHandler}
              onClearAttendance={rowData.clearAttendanceHandler}
              onSetEta={rowData.setEtaHandler}
              onClearEta={rowData.clearEtaHandler}
              defaultEta={defaultEta}
              onSetAllCanCome={setAllAttendanceToCanCome}
              onSetAllCannotCome={setAllAttendanceToCannotCome}
            />
            {!showAttendanceColumns && canShowAttendance && rowData.hasAttendanceData && (
              <QuestionRender
                className={styles.button}
                question={<>
                  <span className={styles.labelDesktop}>Show attendance</span>
                  <span className={styles.labelMobile}>Attendance</span>
                </>}
              >
                <ShowAttendance
                  voters={canShowVoters ? votes[venueId]?.voterUids || [] : []}
                  voterProfiles={voterProfiles}
                  attendanceProfiles={attendanceProfiles}
                  showVoters={canShowVoters}
                  canCome={rowData.canCome}
                  cannotCome={rowData.cannotCome}
                  eta={attendance[venueId]?.eta}
                />
              </QuestionRender>
            )}
          </div>
        </td>
      )}
      {showAttendanceColumns && (
        <>
          {canShowVoters && <td className={styles.attendancePeopleCell}>{votedNames.join(", ")}</td>}
          <td className={styles.attendancePeopleCell}>{canComeNames.join(", ")}</td>
          <td className={styles.attendancePeopleCell}>{cannotComeNames.join(", ")}</td>
          <td className={styles.attendancePeopleCell}>{etaNames.join(", ")}</td>
        </>
      )}
    </tr>
  );
}

/** @param {PollVoteProps} props */
function PollVote(props) {
  /** @type {string | null} */
  const currUserId = useSelector((state) => {
    const typedState = /** @type {RootState} */ (state);
    const uid = typedState.auth?.uid;
    return typeof uid === "string" && uid.length > 0 ? uid : null;
  });
  const allowDelete = useRole("canAddPubToPoll");
  const allowCompletePoll = useRole("canCompletePoll");
  const canShowVoters = useRole("canShowVoters");
  const canRespond = Boolean(currUserId);
  const canShowAttendance = canRespond;
  const canVote = useRole("canVote");
  const canReadProtectedPollData = canRespond;
  const privateData = useUserPrivateData(currUserId);
  const defaultEta = normalizeArrivalTime(privateData?.defaultArrivalTime);
  const voteState = useVotes(props.poll_id, currUserId, canReadProtectedPollData);
  const { optionsById: votes, voterProfiles, selectedOptionIds, pendingOptionIds, retrySelections, errorMessage, changeVote, retryVote } = voteState;
  const [attendance, setAttendanceStatus, clearAttendance, , setGlobalAttendanceStatus, setEta, clearEta, attendanceProfilesResult, attendanceLoading, attendanceError] = useAttendance(
    props.poll_id,
    canReadProtectedPollData
  );
  const attendanceProfiles = attendanceProfilesResult || EMPTY_ATTENDANCE_PROFILES;
  const users = /** @type {Record<string, UserEntry | undefined>} */ (useUsers());
  const mobile = Boolean(props.mobile);
  const tableWrapRef = useRef(null);
  const [isTableOverflowing, setIsTableOverflowing] = useState(false);
  const [isWideForAttendance, setIsWideForAttendance] = useState(!mobile);
  const [wideModeMinWidth, setWideModeMinWidth] = useState(0);
  const showAttendanceColumns = canShowAttendance && !mobile && isWideForAttendance;

  // Get sorted poll rows
  const rowEntries = usePollRows(props.poll_data, props.venues);
  const visibleOptionIds = new Set(rowEntries.map(([, optionId]) => optionId));
  const removedSelections = selectedOptionIds.filter((optionId) =>
    optionId !== "any" && !visibleOptionIds.has(optionId) && !(optionId in votes) && !(optionId in retrySelections)
  );
  const removedOptionRetries = Object.entries(retrySelections)
    .filter(([optionId]) => !visibleOptionIds.has(optionId));

  // Get ballot actions (batch attendance handlers)
  const { pollVenueIds, setAllAttendanceToCanCome, setAllAttendanceToCannotCome } = useBallotActions(
    props.poll_data,
    currUserId,
    setGlobalAttendanceStatus
  );
  const usersByUid = useMemo(() => {
    /** @type {Record<string, UserEntry>} */
    const map = {};
    Object.entries(users).forEach(([key, userEntry]) => {
      if (!userEntry) {
        return;
      }
      const keyUid = normalizeUserId(key);
      const entryUid = normalizeUserId(userEntry.uid);
      if (keyUid) {
        map[keyUid] = userEntry;
      }
      if (entryUid) {
        map[entryUid] = userEntry;
      }
    });
    for (const [uid, profile] of Object.entries(attendanceProfiles)) {
      map[uid] = { uid, name: profile.name };
    }
    return map;
  }, [attendanceProfiles, users]);

  useLayoutEffect(() => {
    const wrap = tableWrapRef.current;
    if (!(wrap instanceof HTMLElement)) {
      return;
    }

    let rafId = 0;
    const syncOverflowState = () => {
      cancelAnimationFrame(rafId);
      rafId = requestAnimationFrame(() => {
        // Treat any positive horizontal delta as overflow; this keeps mode switching symmetric.
        const overflowDelta = wrap.scrollWidth - wrap.clientWidth;
        const hasOverflow = overflowDelta > 0;
        setIsTableOverflowing(hasOverflow);

        const containerWidth =
          (wrap.parentElement instanceof HTMLElement && wrap.parentElement.clientWidth > 0)
            ? wrap.parentElement.clientWidth
            : window.innerWidth;
        if (containerWidth <= 0) {
          // JSDOM and other non-laid-out contexts report zero width.
          setIsWideForAttendance(!mobile);
          return;
        }

        if (mobile) {
          setIsWideForAttendance(false);
          setWideModeMinWidth(0);
          return;
        }

        setIsWideForAttendance((prev) => {
          if (prev) {
            if (hasOverflow) {
              // If wide mode overflows for this dataset, drop to normal mode and
              // require additional room before retrying wide mode.
              const reenterBufferPx = Math.max(32, Math.ceil(containerWidth * 0.05));
              setWideModeMinWidth(containerWidth + reenterBufferPx);
              return false;
            }

            return true;
          }

          return containerWidth >= wideModeMinWidth;
        });
      });
    };

    syncOverflowState();

    let resizeObserver = null;
    if (typeof ResizeObserver !== "undefined") {
      resizeObserver = new ResizeObserver(syncOverflowState);
      resizeObserver.observe(wrap);
      const table = wrap.querySelector("table");
      if (table) {
        resizeObserver.observe(table);
      }
    }

    window.addEventListener("resize", syncOverflowState);
    return () => {
      cancelAnimationFrame(rafId);
      if (resizeObserver) {
        resizeObserver.disconnect();
      }
      window.removeEventListener("resize", syncOverflowState);
    };
  }, [rowEntries.length, canRespond, canShowAttendance, allowDelete, mobile, wideModeMinWidth]);

  return (
    <>
      {attendanceLoading && <p role="status" className="small text-body-secondary mb-2">Loading attendance...</p>}
      {attendanceError && <p role="alert" className="small text-danger mb-2">Attendance unavailable: {attendanceError}</p>}
      <div
        ref={tableWrapRef}
        className={`${styles.tableWrap} ${isTableOverflowing ? styles.tableWrapOverflow : ""}`.trim()}
      >
        <table>
          <thead>
            <tr>
              {allowDelete && <th className={styles.deleteCol}></th>}
              <th className={styles.venueCol}>Venue Name</th>
              <th className={styles.voteCol}>Votes</th>
              {canRespond && <th className={styles.statusCol}></th>}
              {canRespond && <th className={styles.actionsCol}>Actions</th>}
              {showAttendanceColumns && canShowVoters && <th className={styles.attendancePeopleCol}>Voted</th>}
              {showAttendanceColumns && <th className={styles.attendancePeopleCol}>Can come</th>}
              {showAttendanceColumns && <th className={styles.attendancePeopleCol}>Cannot come</th>}
              {showAttendanceColumns && <th className={styles.attendancePeopleCol}>ETA</th>}
            </tr>
          </thead>
          <tbody>
            {rowEntries.map(([venueName, key]) => {
              const isGlobal = key === "any";
              return (
                <VotableVenue
                  key={key}
                  venueId={key}
                  venueName={venueName}
                  venueType={props.venues?.[key]?.venueType || "pub"}
                  currUserId={currUserId}
                  votes={votes}
                  selectedOptionIds={selectedOptionIds}
                  pendingOptionIds={pendingOptionIds}
                  retrySelections={retrySelections}
                  canCastVote={canVote}
                  voterProfiles={voterProfiles}
                  attendance={attendance}
                  canShowAttendance={canShowAttendance}
                  canShowVoters={canShowVoters}
                  changeVote={changeVote}
                  retryVote={retryVote}
                  setAttendanceStatus={setAttendanceStatus}
                  clearAttendance={clearAttendance}
                  setEta={setEta}
                  clearEta={clearEta}
                  setAllAttendanceToCanCome={setAllAttendanceToCanCome}
                  setAllAttendanceToCannotCome={setAllAttendanceToCannotCome}
                  pollVenueIds={pollVenueIds}
                  showDeleteColumn={allowDelete}
                  allowDelete={!isGlobal && allowDelete}
                  allowCompletePoll={!isGlobal && allowCompletePoll}
                  pollId={props.poll_id}
                  pollDate={props.poll_data?.date}
                  completeHandler={() => {
                    props.on_complete(key, venueName, props.poll_id);
                  }}
                  mobile={mobile}
                  showAttendanceColumns={showAttendanceColumns}
                  usersByUid={usersByUid}
                  attendanceProfiles={attendanceProfiles}
                  defaultEta={defaultEta}
                />
              );
            })}
          </tbody>
        </table>
      </div>
      {canVote && removedSelections.length > 0 && (
        <div className="mt-2">
          {removedSelections.map((optionId) => (
            <Button
              key={optionId}
              type="button"
              variant="outline-secondary"
              disabled={pendingOptionIds.includes(optionId)}
              aria-busy={pendingOptionIds.includes(optionId)}
              onClick={() => changeVote(optionId, false)}
            >
              Withdraw vote for {props.venues?.[optionId]?.name || optionId} (removed)
            </Button>
          ))}
        </div>
      )}
      {(errorMessage || removedOptionRetries.length > 0) && (
        <div className="alert alert-warning mt-2" role="status">
          {errorMessage && <p className="mb-2">{errorMessage}</p>}
          {removedOptionRetries.map(([optionId, selected]) => (
            <Button
              key={optionId}
              type="button"
              variant="warning"
              disabled={pendingOptionIds.includes(optionId)}
              aria-busy={pendingOptionIds.includes(optionId)}
              onClick={() => retryVote(optionId)}
              title="Retry a vote change for a removed option"
            >
              {selected ? "Retry vote for removed option" : "Retry withdrawal for a removed option"}
            </Button>
          ))}
        </div>
      )}
    </>
  );
}

export default PollVote;
