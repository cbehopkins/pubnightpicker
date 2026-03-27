import { useSelector } from "react-redux";
import styles from "./PollVote.module.css";
import useVotes from "../../hooks/useVotes";
import { useCallback, useMemo } from "react";
import useAttendance from "../../hooks/useAttendance";
import useRole from "../../hooks/useRole";
import ShowAttendance from "./ShowAttendance";
import AttendanceActions from "./AttendanceActions";
import { QuestionRender } from "./ConfirmModal";
import { deletePubFromPoll } from "../../dbtools/polls";
import { getUserFacingErrorMessage } from "../../permissions";
import { notifyError } from "../../utils/notify";
import { runAttendanceAction } from "../../utils/attendance";

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

  // Define state variables first
  const canVote = Boolean(currUserId);
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
  const hasAttendanceData = voteCount > 0 || canCome.length > 0 || cannotCome.length > 0;

  const voteHandler = useCallback(async () => {
    if (!currUserId) {
      return;
    }
    // If already voted, cancel the vote; otherwise vote
    if (votedFor) {
      await clearVote(pubId, currUserId);
    } else {
      await makeVote(pubId, currUserId);
    }
  }, [makeVote, clearVote, pubId, currUserId, votedFor]);

  const setAttendanceStatusHandler = useCallback(async (status) => {
    if (!currUserId || pubId === "any") {
      return;
    }

    await runAttendanceAction(() => setAttendanceStatus(pubId, currUserId, status));
  }, [currUserId, pubId, setAttendanceStatus]);

  const clearAttendanceHandler = useCallback(async () => {
    if (!currUserId || pubId === "any") {
      return;
    }

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
      {allow_delete && (
        <td>
          <button
            className={styles.deleter}
            onClick={deleteHandler}
          >
            Delete
          </button>
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
        <td>
          <button
            className={`${styles.voteButton} ${votedFor ? styles.voted : ""}`}
            onClick={voteHandler}
            title={votedFor ? "Cancel your vote" : "Vote for this pub"}
          >
            {votedFor ? "Cancel Vote" : "Vote Up"}
          </button>
        </td>
      )}
      {allowAttendanceControls && <td>
        <AttendanceActions
          className={styles.attendanceCell}
          buttonClassName={styles.attendanceButton}
          canComeSelectedClassName={styles.attending}
          cannotComeSelectedClassName={styles.notAttending}
          canComeSelected={userCanCome}
          cannotComeSelected={userCannotCome}
          clearMode="toggle"
          onSetStatus={setAttendanceStatusHandler}
          onClear={clearAttendanceHandler}
        />
      </td>}
      {canVote && pubId === "any" && <td><label>-</label></td>}
      {canShowAttendance && <td>
        {hasAttendanceData && <QuestionRender
          className={styles.button}
          question="Show attendance"
        >
          <ShowAttendance
            voters={votes[pubId] || []}
            canCome={canCome}
            cannotCome={cannotCome}
          />
        </QuestionRender>}
      </td>}
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

  const sortedPubsByName = props.poll_data.pubs && Object.entries(props.poll_data.pubs)
    .map(([id, pub]) => {
      const sortBy = pub.name;
      return [sortBy, id, pub];
    })
    .sort();

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

  return (
    <>
      {canVote && pollPubIds.length > 0 && <div className={styles.globalAttendanceActions}>
        <button className={styles.comingAllButton} onClick={setAllAttendanceToCanCome}>
          I'm coming
        </button>
        <button className={styles.cannotComeAllButton} onClick={setAllAttendanceToCannotCome}>
          I can't come
        </button>
      </div>}
      <table>
        <thead>
          <tr>
            {allowDelete && <th></th>}
            <th>Pub Name</th>
            <th>Vote Count</th>
            {canVote && <th>Vote</th>}
            {canVote && <th>Attendance</th>}
            {canShowAttendance && <th></th>}
          </tr>
        </thead>
        <tbody>
          <VotablePub
            pub_id="any"
            pub_name="Any is good"
            poll_id={props.poll_id}
            user_id={currUserId}
            allow_delete={false}
            allow_complete={false}
            votes={votes}
            attendance={attendance}
            can_show_attendance={canShowAttendance}
            make_vote={makeVote}
            clear_vote={clearVote}
            set_attendance_status={setAttendanceStatus}
            clear_attendance={clearAttendance}
          />
          {props.poll_data.pubs &&
            sortedPubsByName.map(([, key]) => {
              if (key === "any") {
                return <></>;
              }
              const pubName = props.pub_parameters[key].name;
              return (
                <VotablePub
                  key={key}
                  pub_id={key}
                  pub_name={pubName}
                  poll_id={props.poll_id}
                  user_id={currUserId}
                  allow_delete={allowDelete}
                  allow_complete={allowCompletePoll}
                  votes={votes}
                  attendance={attendance}
                  can_show_attendance={canShowAttendance}
                  make_vote={makeVote}
                  clear_vote={clearVote}
                  set_attendance_status={setAttendanceStatus}
                  clear_attendance={clearAttendance}
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
