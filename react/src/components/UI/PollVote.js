import { useSelector } from "react-redux";
import styling from "./PollVote.module.css";
import useAdmin from "../../hooks/useAdmin";
import useVotes from "../../hooks/useVotes";
import { useCallback } from "react";
import useKnown from "../../hooks/useKnown";
import ShowVoters from "./ShowVoters";
import { QuestionRender } from "./ConfirmModal";
import { deletePubFromPoll } from "../../dbtools/polls"

function VotablePub(params) {
  const known = useKnown();
  const pubName = params.pub_name;
  const currUserId = params.user_id;
  const pubId = params.pub_id;
  const pollId = params.poll_id;
  const [votes, makeVote, clearVote] = useVotes(pollId);

  const voteHandler = useCallback(async () => {
    if (!currUserId) {
      return;
    }
    await makeVote(pubId, currUserId);
  }, [makeVote, pubId, currUserId]);
  const downvoteHandler = useCallback(async () => {
    if (!currUserId) {
      return;
    }
    await clearVote(pubId, currUserId);
  }, [clearVote, pubId, currUserId]);
  const deleteHandler = useCallback(async () => {
    if (!currUserId) {
      return;
    }
    await deletePubFromPoll(pollId, pubId);
  }, [pollId, pubId, currUserId]);
  const canVote = Boolean(currUserId);
  const allow_delete = params.allow_delete;
  const allow_complete = params.allow_complete;
  const voteCount = pubId in votes ? votes[pubId].length : 0;
  const votedFor = pubId in votes && votes[pubId].includes(currUserId);
  const notVotedFor = pubId in votes && !votes[pubId].includes(currUserId);
  return (
    <tr>
      {allow_delete ? (
        <td>
          <button
            className={styling.deleter}
            onClick={deleteHandler}
          >
            Delete
          </button>
        </td>
      ) : (
        <td></td>
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
            disabled={votedFor}
            onClick={(event) => {
              event.preventDefault();
              voteHandler();
            }}
          >
            Vote Up
          </button>
        </td>
      )}
      {canVote && (
        <td>
          <button
            disabled={notVotedFor}
            onClick={(event) => {
              event.preventDefault();
              downvoteHandler();
            }}
          >
            Vote Down
          </button>
        </td>
      )}
      {known && <td><QuestionRender
        className={styling.button}
        question="Show Voters"
      >
        <ShowVoters votes={votes[pubId]} />
      </QuestionRender></td>}
    </tr>
  );
}

function PollVote(props) {
  const admin = useAdmin();
  const loggedIn = useSelector((state) => state.auth.loggedIn);
  const currUserId = useSelector((state) => state.auth.uid);
  const allowDelete = loggedIn && admin;
  const dummyAnyPub = (props.poll_data.pubs &&
    "any" in props.poll_data.pubs &&
    props.poll_data.pubs["any"]) || { name: "All Pubs" };
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
          value={dummyAnyPub}
          user_id={currUserId}
          allow_delete={false}
          allow_complete={false}
          className={props.className}
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
                value={value}
                user_id={currUserId}
                allow_delete={allowDelete}
                allow_complete={allowDelete}
                complete_handler={() => {
                  props.on_complete(key, pubName, props.poll_id);
                }}
                className={props.className}
              />
            );
          })}
      </tbody>
    </table>
  );
}

export default PollVote;
