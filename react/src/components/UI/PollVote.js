import { useSelector } from "react-redux";
import styles from "./PollVote.module.css";
import useVotes from "../../hooks/useVotes";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import useAttendance from "../../hooks/useAttendance";
import useRole from "../../hooks/useRole";
import ShowAttendance from "./ShowAttendance";
import AttendanceActions from "./AttendanceActions";
import { QuestionRender } from "./ConfirmModal";
import { deletePubFromPoll } from "../../dbtools/polls";
import { getUserFacingErrorMessage } from "../../permissions";
import { notifyError } from "../../utils/notify";
import { runAttendanceAction } from "../../utils/attendance";

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
  const [panelPlacement, setPanelPlacement] = useState("below");
  const menuRef = useRef(null);
  const triggerRef = useRef(null);
  const panelRef = useRef(null);

  useEffect(() => {
    if (!showActions || !panelRef.current || !triggerRef.current) {
      return;
    }

    const triggerRect = triggerRef.current.getBoundingClientRect();
    const panelRect = panelRef.current.getBoundingClientRect();
    const spaceRight = window.innerWidth - triggerRect.right;
    const spaceAbove = triggerRect.top;
    const spaceBelow = window.innerHeight - triggerRect.bottom;

    if (spaceBelow < panelRect.height + 12 && spaceAbove >= panelRect.height + 12) {
      setPanelPlacement("above");
      return;
    }

    if (spaceRight >= panelRect.width + 12) {
      setPanelPlacement("right");
      return;
    }

    setPanelPlacement("below");
  }, [showActions, allowAttendanceControls]);

  useEffect(() => {
    if (!showActions) {
      return;
    }

    const closeIfOutside = (event) => {
      if (menuRef.current && !menuRef.current.contains(event.target)) {
        setShowActions(false);
      }
    };

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
      <button
        ref={triggerRef}
        type="button"
        className={`${styles.respondSummary} ${showActions ? styles.respondSummaryActive : ""}`}
        onClick={toggleActions}
        aria-haspopup="menu"
        aria-expanded={showActions}
      >
        Respond
      </button>
      {showActions && (
        <div
          ref={panelRef}
          className={`${styles.respondPanel} ${
            panelPlacement === "right"
              ? styles.respondPanelRight
              : panelPlacement === "above"
                ? styles.respondPanelAbove
                : styles.respondPanelBelow
          }`}
        >
          <button
            className={`${styles.voteButton} ${votedFor ? styles.voted : ""}`}
            onClick={async () => {
              await onVote();
              setShowActions(false);
            }}
            title={votedFor ? "Cancel your vote" : "Vote for this pub"}
          >
            {votedFor ? "Cancel Vote" : "Vote Up"}
          </button>
          {allowAttendanceControls && (
            <AttendanceActions
              canComeSelected={userCanCome}
              cannotComeSelected={userCannotCome}
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
              <button
                className={styles.comingAllButton}
                onClick={async () => {
                  await onSetAllCanCome();
                  setShowActions(false);
                }}
                title="Set can come for every pub in this poll"
              >
                I'm coming (all pubs)
              </button>
              <button
                className={styles.cannotComeAllButton}
                onClick={async () => {
                  await onSetAllCannotCome();
                  setShowActions(false);
                }}
                title="Set cannot come for every pub in this poll"
              >
                I can't come (all pubs)
              </button>
            </>
          )}
        </div>
      )}
    </div>
  );
}

function VotablePub(params) {
  const pubName = params.pub_name;
  const currUserId = params.user_id;
  const pubId = params.pub_id;
  const pollId = params.poll_id;
  const votes = params.votes;
  const attendance = params.attendance;
  const canShowAttendance = params.can_show_attendance;
  const makeVote = params.make_vote;
  const clearVote = params.clear_vote;
  const setAttendanceStatus = params.set_attendance_status;
  const clearAttendance = params.clear_attendance;
  const setAllAttendanceToCanCome = params.set_all_attendance_to_can_come;
  const setAllAttendanceToCannotCome = params.set_all_attendance_to_cannot_come;
  const pollPubIds = params.poll_pub_ids || [];

  const canVote = Boolean(currUserId);
  const showDeleteColumn = params.show_delete_column;
  const allow_delete = params.allow_delete;
  const allow_complete = params.allow_complete;
  const voteCount = pubId in votes ? votes[pubId].length : 0;
  const votedFor = pubId in votes && votes[pubId].includes(currUserId);
  const attendanceForPub = attendance[pubId] || {};
  const canCome = attendanceForPub.canCome || [];
  const cannotCome = attendanceForPub.cannotCome || [];
  const userCanCome = Boolean(currUserId) && canCome.includes(currUserId);
  const userCannotCome = Boolean(currUserId) && cannotCome.includes(currUserId);
  const allowAttendanceControls = canVote && pubId !== "any";
  const allowGlobalAttendanceControls = canVote && pubId === "any" && pollPubIds.length > 0;
  const hasAttendanceData = voteCount > 0 || canCome.length > 0 || cannotCome.length > 0;

  const voteHandler = useCallback(async () => {
    if (!currUserId) return;
    if (votedFor) {
      await clearVote(pubId, currUserId);
    } else {
      await makeVote(pubId, currUserId);
    }
  }, [makeVote, clearVote, pubId, currUserId, votedFor]);

  const setAttendanceStatusHandler = useCallback(async (status) => {
    if (!currUserId || pubId === "any") return;
    await runAttendanceAction(() => setAttendanceStatus(pubId, currUserId, status));
  }, [currUserId, pubId, setAttendanceStatus]);

  const clearAttendanceHandler = useCallback(async () => {
    if (!currUserId || pubId === "any") return;
    await runAttendanceAction(() => clearAttendance(pubId, currUserId));
  }, [clearAttendance, currUserId, pubId]);

  const deleteHandler = useCallback(async () => {
    try {
      await deletePubFromPoll(pollId, pubId);
    } catch (error) {
      notifyError(getUserFacingErrorMessage(error, "Unable to remove this pub from the poll."));
    }
  }, [pollId, pubId]);

  return (
    <tr>
      {showDeleteColumn && (
        <td>
          {allow_delete && (
            <button
              className={styles.deleter}
              onClick={deleteHandler}
            >
              Delete
            </button>
          )}
        </td>
      )}
      <td>
        {allow_complete ? (
          <button onClick={params.complete_handler}>{pubName}</button>
        ) : (
          <label>{pubName}</label>
        )}
      </td>
      <td>
        <label>{voteCount}</label>
      </td>
      {canVote && (
        <td className={styles.attendanceIndicator}>
          {pubId !== "any" && (
            userCanCome ? <span title="You said you can come">✅</span>
            : userCannotCome ? <span title="You said you cannot come">❌</span>
            : null
          )}
        </td>
      )}

      {canVote && (
        <td>
          <RespondMenu
            votedFor={votedFor}
            userCanCome={userCanCome}
            userCannotCome={userCannotCome}
            allowAttendanceControls={allowAttendanceControls}
            allowGlobalAttendanceControls={allowGlobalAttendanceControls}
            onVote={voteHandler}
            onSetAttendanceStatus={setAttendanceStatusHandler}
            onClearAttendance={clearAttendanceHandler}
            onSetAllCanCome={setAllAttendanceToCanCome}
            onSetAllCannotCome={setAllAttendanceToCannotCome}
          />
        </td>
      )}

      {canShowAttendance && (
        <td>
          {hasAttendanceData && (
            <QuestionRender
              className={styles.button}
              question="Show attendance"
            >
              <ShowAttendance
                voters={votes[pubId] || []}
                canCome={canCome}
                cannotCome={cannotCome}
              />
            </QuestionRender>
          )}
        </td>
      )}
    </tr>
  );
}

function PollVote(props) {
  const currUserId = useSelector((state) => state.auth.uid);
  const allowDelete = useRole("canAddPubToPoll");
  const allowCompletePoll = useRole("canCompletePoll");
  const canShowAttendance = useRole("canShowVoters");
  const [votes, makeVote, clearVote] = useVotes(props.poll_id);
  const [attendance, setAttendanceStatus, clearAttendance, setAttendanceForMultiplePubs] = useAttendance(props.poll_id);
  const canVote = Boolean(currUserId);

  const pollPubIds = useMemo(() => {
    return Object.keys(props.poll_data.pubs || {}).filter((pubId) => pubId !== "any");
  }, [props.poll_data.pubs]);

  const setAllAttendanceToCanCome = useCallback(async () => {
    if (!currUserId || pollPubIds.length === 0) {
      return;
    }

    await runAttendanceAction(() => setAttendanceForMultiplePubs(pollPubIds, currUserId, "canCome"));
  }, [currUserId, pollPubIds, setAttendanceForMultiplePubs]);

  const setAllAttendanceToCannotCome = useCallback(async () => {
    if (!currUserId || pollPubIds.length === 0) {
      return;
    }

    await runAttendanceAction(() => setAttendanceForMultiplePubs(pollPubIds, currUserId, "cannotCome"));
  }, [currUserId, pollPubIds, setAttendanceForMultiplePubs]);

  const rowEntries = useMemo(() => {
    const rows = [["Global", "any"]];

    const pubRows = [];
    if (props.poll_data.pubs) {
      for (const [id, pub] of Object.entries(props.poll_data.pubs)) {
        if (id === "any") {
          continue;
        }
        pubRows.push([pub.name, id]);
      }
    }

    pubRows.sort((a, b) => a[0].localeCompare(b[0]));
    rows.push(...pubRows);
    return rows;
  }, [props.poll_data.pubs]);

  return (
    <>
      <table>
        <thead>
          <tr>
            {allowDelete && <th></th>}
            <th>Pub Name</th>
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
                  pub_id={key}
                  pub_name={pubName}
                  poll_id={props.poll_id}
                  user_id={currUserId}
                  show_delete_column={allowDelete}
                  allow_delete={!isGlobal && allowDelete}
                  allow_complete={!isGlobal && allowCompletePoll}
                  votes={votes}
                  attendance={attendance}
                  can_show_attendance={canShowAttendance}
                  make_vote={makeVote}
                  clear_vote={clearVote}
                  set_attendance_status={setAttendanceStatus}
                  clear_attendance={clearAttendance}
                  set_all_attendance_to_can_come={setAllAttendanceToCanCome}
                  set_all_attendance_to_cannot_come={setAllAttendanceToCannotCome}
                  poll_pub_ids={pollPubIds}
                  complete_handler={() => {
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
