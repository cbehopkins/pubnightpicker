import { useState, useCallback } from "react";
import { useSelector } from "react-redux";
import { usePastCompletePolls, useFutureCompletePolls } from "../../hooks/usePolls";
import Modal from "../UI/Modal";
import PubOptions from "../UI/PubOptions";
import usePubs from "../../hooks/usePubs";
import useVotes from "../../hooks/useVotes";
import useAttendance from "../../hooks/useAttendance";
import useRole from "../../hooks/useRole";
import styles from "./CurrentEvents.module.css";
import ShowAttendance from "../UI/ShowAttendance";
import AttendanceActions from "../UI/AttendanceActions";
import ConfirmModal, { QuestionRender } from "../UI/ConfirmModal";
import { deletePoll, reschedule_a_poll } from "../../dbtools/polls";
import { getUserFacingErrorMessage } from "../../permissions";
import { notifyError } from "../../utils/notify";
import { runAttendanceAction } from "../../utils/attendance";

function PastEvent({ value, pub_parameters }) {
  if (!pub_parameters[value.selected]) {
    return <div></div>;
  }
  const pubName = pub_parameters[value.selected].name;
  const pubWebsite = pub_parameters[value.selected]?.web_site
  const pubImage = pub_parameters[value.selected]?.pubImage
  return (
    <>
      <h2>{pubName}</h2>
      <h3>{value.date}</h3>
      {/* Open link in new window with _blank magic */}
      {pubWebsite && <p><a href={pubWebsite} target="_blank" rel="noreferrer">Pub Website</a></p>}
      {pubImage && <img
        src={pubImage}
        alt="What the pub looks like"
        className={styles.image}
      />}
    </>
  );
}

export function PastEvents() {
  const [pubCount, setPubCount] = useState(5);
  const pollData = usePastCompletePolls(pubCount);
  const pub_parameters = usePubs();
  const sortedPollsByDate = [...pollData.sortedByDate(true)]
  const selectPubCountHandler = useCallback(
    (event) => {
      event.preventDefault();
      console.log("setting value to", event.target.value)
      setPubCount(event.target.value);
    },
    [setPubCount]
  );
  return (
    <div>
      <h1>Past Events</h1>
      <span>
        <label>Number of past events to show</label>
        <select defaultValue={pubCount} onChange={selectPubCountHandler}>
          <option>5</option>
          <option>10</option>
          <option>20</option>
        </select></span>
      {sortedPollsByDate.map(([key, value]) => {
        return <PastEvent key={key} value={value} pub_parameters={pub_parameters} />
      })}
    </div>
  );
}
function ChangePubModal(params) {
  const [selectedPub, setSelectedPub] = useState("");

  const selectPubHandler = useCallback(
    (event) => {
      event.preventDefault();
      setSelectedPub(event.target.value);
    },
    [setSelectedPub]
  );

  return (
    <Modal>
      <div>
        <h1>Change to a different pub?</h1>
        <PubOptions
          pub_parameters={params.pub_parameters}
          optionText="New Pub"
          selectPubHandler={selectPubHandler}
        />
      </div>
      <div>
        <button
          onClick={() => {
            console.log("Dispatching Change Pub event")
            params.on_confirm(selectedPub);
          }}
        >
          Reschedule
        </button>
        <button onClick={params.on_cancel}>Cancel</button>
      </div>
    </Modal>
  );
}


function CurrentEvent({ poll_id, current_pub_id, date, pub_parameters, can_reschedule, can_delete_event, show_voters }) {
  const currUserId = useSelector((state) => state.auth.uid);
  const [votes] = useVotes(poll_id);
  const [attendance, setAttendanceStatus, clearAttendance] = useAttendance(poll_id);
  const pubWasVotedFor = current_pub_id in votes || Boolean(votes["any"]);
  const pubName = pub_parameters[current_pub_id].name;
  const pubWebsite = pub_parameters[current_pub_id]?.web_site
  const pubImage = pub_parameters[current_pub_id]?.pubImage
  const pubAddress = pub_parameters[current_pub_id]?.address
  const attendanceForPub = attendance[current_pub_id] || {};
  const canCome = attendanceForPub.canCome || [];
  const cannotCome = attendanceForPub.cannotCome || [];
  const userCanCome = Boolean(currUserId) && canCome.includes(currUserId);
  const userCannotCome = Boolean(currUserId) && cannotCome.includes(currUserId);
  const currentVotes = []
  if (votes[current_pub_id]) {
    currentVotes.push(...votes[current_pub_id])
  }
  if (votes["any"]) {
    currentVotes.push(...votes["any"])
  }
  const dedupedVotes = [...new Set(currentVotes)];
  const hasAttendanceData = canCome.length > 0 || cannotCome.length > 0;
  const allowShowVoters = show_voters && (pubWasVotedFor || hasAttendanceData);
  const rescheduleHandler = async (pubId) => {
    try {
      return await reschedule_a_poll(poll_id, current_pub_id, pubId);
    } catch (error) {
      notifyError(getUserFacingErrorMessage(error, "Unable to reschedule this event."));
    }
  };
  const setAttendanceStatusHandler = async (status) => {
    if (!currUserId) {
      return;
    }

    await runAttendanceAction(() => setAttendanceStatus(current_pub_id, currUserId, status));
  };

  const clearAttendanceHandler = async () => {
    if (!currUserId || (!userCanCome && !userCannotCome)) {
      return;
    }

    await runAttendanceAction(
      () => clearAttendance(current_pub_id, currUserId),
      "Unable to clear your attendance.",
    );
  };
  return (
    <>
      {pubWebsite ? <h2><a href={pubWebsite} target="_blank" rel="noreferrer">{pubName}</a></h2> : <h2>{pubName}</h2>}
      <h3>{date}</h3>
      {pubAddress && <p>{pubAddress}</p>}
      {pubImage && <p><img
        src={pubImage}
        alt="What the pub looks like"
        className={styles.image}
      /></p>}
      {currUserId && <AttendanceActions
        className={styles.attendanceActions}
        buttonClassName={styles.attendanceButton}
        selectedClassName={styles.attendanceButtonSelected}
        canComeSelected={userCanCome}
        cannotComeSelected={userCannotCome}
        canComeSelectedLabel="Can come confirmed"
        cannotComeSelectedLabel="Cannot come confirmed"
        clearMode="button"
        onSetStatus={setAttendanceStatusHandler}
        onClear={clearAttendanceHandler}
      />}
      {can_reschedule && (
        <QuestionRender
          className={styles.button}
          question="Reschedule Event"
        >
          <ChangePubModal
            pub_parameters={pub_parameters}
            on_confirm={rescheduleHandler}
          />
        </QuestionRender>
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
      {allowShowVoters && <QuestionRender className={styles.button} question="Show attendance">
        <ShowAttendance voters={dedupedVotes} canCome={canCome} cannotCome={cannotCome} />
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
  return (
    <div>
      <h1>Current Events</h1>
      {[...pollData.sortedByDate()].map(([key, value]) => {
        return (
          <CurrentEvent
            key={key}
            poll_id={key}
            current_pub_id={value.selected}
            date={value.date}
            pub_parameters={pub_parameters}
            can_reschedule={canReschedule}
            can_delete_event={canDeleteEvent}
            show_voters={canShowVoters}
          />
        );
      })}
    </div>
  );
}
export default CurrentEvents;
