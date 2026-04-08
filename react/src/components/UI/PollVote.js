// @ts-check

import { useSelector } from "react-redux";
import styles from "./PollVote.module.css";
import useVotes from "../../hooks/useVotes";
import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import useAttendance from "../../hooks/useAttendance";
import useRole from "../../hooks/useRole";
import { useVotableRow } from "../../hooks/useVotableRow";
import { usePollRows } from "../../hooks/usePollRows";
import { useBallotActions } from "../../hooks/useBallotActions";
import ShowAttendance from "./ShowAttendance";
import AttendanceActions from "./AttendanceActions";
import { QuestionRender } from "./ConfirmModal";
import Button from "./Button";

/** @typedef {import("../../store").RootState} RootState */

/** @typedef {Record<string, string[]>} VotesMap */
/** @typedef {Record<string, { canCome?: string[], cannotCome?: string[] } | undefined>} AttendanceMap */
/** @typedef {{ name?: string }} PollPubEntry */
/** @typedef {{ pubs?: Record<string, PollPubEntry> }} PollData */

/**
 * @typedef {Object} RespondMenuProps
 * @property {boolean} votedFor
 * @property {boolean} userCanCome
 * @property {boolean} userCannotCome
 * @property {boolean} allowAttendanceControls
 * @property {boolean} allowGlobalAttendanceControls
 * @property {() => Promise<void>} onVote
 * @property {(status: "canCome" | "cannotCome") => Promise<void>} onSetAttendanceStatus
 * @property {() => Promise<void>} onClearAttendance
 * @property {() => Promise<void>} onSetAllCanCome
 * @property {() => Promise<void>} onSetAllCannotCome
 */

/**
 * @typedef {Object} VotablePubProps
 * @property {string} pubId
 * @property {string} pubName
 * @property {string | null | undefined} currUserId
 * @property {VotesMap} votes
 * @property {AttendanceMap} attendance
 * @property {boolean} canShowAttendance
 * @property {(pubId: string, userId: string) => Promise<void>} makeVote
 * @property {(pubId: string, userId: string) => Promise<void>} clearVote
 * @property {(pubId: string, userId: string, status: "canCome" | "cannotCome") => Promise<void>} setAttendanceStatus
 * @property {(pubId: string, userId: string) => Promise<void>} clearAttendance
 * @property {() => Promise<void>} setAllAttendanceToCanCome
 * @property {() => Promise<void>} setAllAttendanceToCannotCome
 * @property {string[]} pollPubIds
 * @property {boolean} showDeleteColumn
 * @property {boolean} allowDelete
 * @property {boolean} allowCompletePoll
 * @property {string} pollId
 * @property {() => void} completeHandler
 */

/**
 * @typedef {Object} PollVoteProps
 * @property {string} poll_id
 * @property {PollData} poll_data
 * @property {(pubId: string, pubName: string, pollId: string) => void} on_complete
 */

/** @param {RespondMenuProps} props */
function RespondMenu({
  votedFor,
  userCanCome,
  userCannotCome,
  allowAttendanceControls,
  allowGlobalAttendanceControls,
  onVote,
  onSetAttendanceStatus,
  onClearAttendance,
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
        top = triggerRect.bottom + gutter;
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
          <Button
            type="button"
            variant={votedFor ? "warning" : "success"}
            className={`${styles.voteButton} ${votedFor ? styles.voted : ""}`}
            onClick={async () => {
              await onVote();
              setShowActions(false);
            }}
            title={votedFor ? "Cancel your vote" : "Vote for this venue"}
          >
            {votedFor ? "Cancel Vote" : "Vote Up"}
          </Button>
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
                title="Set can come for every pub in this poll"
              >
                I'm coming (all pubs)
              </Button>
              <Button
                type="button"
                variant="danger"
                className={styles.cannotComeAllButton}
                onClick={async () => {
                  await onSetAllCannotCome();
                  setShowActions(false);
                }}
                title="Set cannot come for every pub in this poll"
              >
                I can't come (all pubs)
              </Button>
            </>
          )}
        </div>
      )}
    </div>
  );
}

/** @param {VotablePubProps} props */
function VotablePub({
  pubId,
  pubName,
  currUserId,
  votes,
  attendance,
  canShowAttendance,
  makeVote,
  clearVote,
  setAttendanceStatus,
  clearAttendance,
  setAllAttendanceToCanCome,
  setAllAttendanceToCannotCome,
  pollPubIds,
  showDeleteColumn,
  allowDelete,
  allowCompletePoll,
  pollId,
  completeHandler,
}) {
  const rowData = useVotableRow(
    pubId,
    currUserId,
    votes,
    attendance,
    setAttendanceStatus,
    clearAttendance,
    makeVote,
    clearVote,
    pollId
  );

  // Override allowGlobalAttendanceControls to include pollPubIds length check
  const allowGlobalAttendanceControls = rowData.canVote && pubId === "any" && pollPubIds.length > 0;

  return (
    <tr>
      {showDeleteColumn && (
        <td>
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
      <td>
        {allowCompletePoll ? (
          <Button type="button" variant="secondary" onClick={completeHandler} title="Select this venue as the winner to complete the poll">{pubName}</Button>
        ) : (
          <label>{pubName}</label>
        )}
      </td>
      <td>
        <label>{rowData.voteCount}</label>
      </td>
      {rowData.canVote && (
        <td className={styles.attendanceIndicator}>
          {pubId !== "any" && (
            rowData.userCanCome ? <span title="You said you can come">✅</span>
              : rowData.userCannotCome ? <span title="You said you cannot come">❌</span>
                : null
          )}
        </td>
      )}

      {rowData.canVote && (
        <td>
          <RespondMenu
            votedFor={rowData.votedFor}
            userCanCome={rowData.userCanCome}
            userCannotCome={rowData.userCannotCome}
            allowAttendanceControls={rowData.allowAttendanceControls}
            allowGlobalAttendanceControls={allowGlobalAttendanceControls}
            onVote={rowData.voteHandler}
            onSetAttendanceStatus={rowData.setAttendanceStatusHandler}
            onClearAttendance={rowData.clearAttendanceHandler}
            onSetAllCanCome={setAllAttendanceToCanCome}
            onSetAllCannotCome={setAllAttendanceToCannotCome}
          />
        </td>
      )}

      {canShowAttendance && (
        <td>
          {rowData.hasAttendanceData && (
            <QuestionRender
              className={styles.button}
              question="Show attendance"
            >
              <ShowAttendance
                voters={votes[pubId] || []}
                canCome={rowData.canCome}
                cannotCome={rowData.cannotCome}
              />
            </QuestionRender>
          )}
        </td>
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
  const canShowAttendance = useRole("canShowVoters");
  const canVote = Boolean(currUserId);
  const canReadProtectedPollData = canVote;
  const [votes, makeVote, clearVote] = useVotes(props.poll_id, canReadProtectedPollData);
  const [attendance, setAttendanceStatus, clearAttendance, , setGlobalAttendanceStatus] = useAttendance(
    props.poll_id,
    canReadProtectedPollData
  );

  // Get sorted poll rows
  const rowEntries = usePollRows(props.poll_data);

  // Get ballot actions (batch attendance handlers)
  const { pollPubIds, setAllAttendanceToCanCome, setAllAttendanceToCannotCome } = useBallotActions(
    props.poll_data,
    currUserId,
    setGlobalAttendanceStatus
  );

  return (
    <>
      <table>
        <thead>
          <tr>
            {allowDelete && <th></th>}
            <th>Venue Name</th>
            <th>Votes</th>
            {canVote && <th></th>}
            {canVote && <th>Actions</th>}
            {canShowAttendance && <th></th>}
          </tr>
        </thead>
        <tbody>
          {rowEntries.map(([pubName, key]) => {
            const isGlobal = key === "any";
            return (
              <VotablePub
                key={key}
                pubId={key}
                pubName={pubName}
                currUserId={currUserId}
                votes={votes}
                attendance={attendance}
                canShowAttendance={canShowAttendance}
                makeVote={makeVote}
                clearVote={clearVote}
                setAttendanceStatus={setAttendanceStatus}
                clearAttendance={clearAttendance}
                setAllAttendanceToCanCome={setAllAttendanceToCanCome}
                setAllAttendanceToCannotCome={setAllAttendanceToCannotCome}
                pollPubIds={pollPubIds}
                showDeleteColumn={allowDelete}
                allowDelete={!isGlobal && allowDelete}
                allowCompletePoll={!isGlobal && allowCompletePoll}
                pollId={props.poll_id}
                completeHandler={() => {
                  props.on_complete(key, pubName, props.poll_id);
                }}
              />
            );
          })}
        </tbody>
      </table>
    </>
  );
}

export default PollVote;
