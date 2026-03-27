import { useSelector } from "react-redux";
import styles from "./PollVote.module.css";
import useVotes from "../../hooks/useVotes";
import { useCallback } from "react";
import useRole from "../../hooks/useRole";
import ShowVoters from "./ShowVoters";
import { QuestionRender } from "./ConfirmModal";
import { deletePubFromPoll } from "../../dbtools/polls";
import { getUserFacingErrorMessage } from "../../permissions";
import { notifyError } from "../../utils/notify";

function VotablePub(params) {
  const canShowVoters = useRole("canShowVoters");
  const pubName = params.pub_name;
  const currUserId = params.user_id;
  const pubId = params.pub_id;
  const pollId = params.poll_id;
  const [votes, makeVote, clearVote] = useVotes(pollId);

  // Define state variables first
  const canVote = Boolean(currUserId);
  const allow_delete = params.allow_delete;
  const allow_complete = params.allow_complete;
  const voteCount = pubId in votes ? votes[pubId].length : 0;
  const votedFor = pubId in votes && votes[pubId].includes(currUserId);

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
      {canShowVoters && <td><QuestionRender
        className={styles.button}
        question="Show Voters"
      >
        <ShowVoters votes={votes[pubId]} />
      </QuestionRender></td>}
    </tr>
  );
}

function PollVote(props) {
  const currUserId = useSelector((state) => state.auth.uid);
  const allowDelete = useRole("canAddPubToPoll");
  const allowCompletePoll = useRole("canCompletePoll");
  const sortedPubsByName = props.poll_data.pubs && Object.entries(props.poll_data.pubs)
    .map(([id, pub]) => {
      const sortBy = pub.name;
      return [sortBy, id, pub];
    })
    .sort();
  return (
    <table>
      <thead>
        <tr>
          {allowDelete && <th></th>}
          <th>Pub Name</th>
          <th>Vote Count</th>
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
        />
        {props.poll_data.pubs &&
          sortedPubsByName.map(([, key, value]) => {
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
                complete_handler={() => {
                  props.on_complete(key, pubName, props.poll_id);
                }}
              />
            );
          })}
      </tbody>
    </table>
  );
}

export default PollVote;
