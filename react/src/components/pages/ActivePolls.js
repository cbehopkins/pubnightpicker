import React, { useState, useCallback, useEffect } from "react";
import styles from "./ActivePolls.module.css";
import usePolls from "../../hooks/usePolls";
import usePubs from "../../hooks/usePubs";
import ConfirmModal from "../UI/ConfirmModal";
import ActivePoll from "../UI/ActivePoll";
import NewPoll from "../UI/NewPoll";
import useRole from "../../hooks/useRole";
import { complete_a_poll } from "../../dbtools/polls";
import { getUserFacingErrorMessage } from "../../permissions";
import { notifyError } from "../../utils/notify";


function ActivePolls() {
  const [mobile, setMobile] = useState(window.innerWidth <= 800);
  const canCreatePoll = useRole("canCreatePoll");
  const canCompletePoll = useRole("canCompletePoll");

  const handleWindowSizeChange = useCallback(() => {
    setMobile(window.innerWidth <= 800);
  }, [setMobile]);

  useEffect(() => {
    window.addEventListener("resize", handleWindowSizeChange);
    // FIXME this listener does not work!
    // I think, Because the new one can be created before the old one is removed
    return () => {
      window.removeEventListener("resize", handleWindowSizeChange);
    };
  }, [handleWindowSizeChange]);

  const pollData = usePolls();
  const pubs = usePubs();
  const currentPollDates = pollData.dates;

  // We have a 2 stage approach, the click handler updates the
  // completingPoll state
  const [completingPoll, setCompletingPoll] = useState([]);
  const completeHandler = useCallback(
    (key, pubName, poll_id) => {
      if (!canCompletePoll) {
        return;
      }
      setCompletingPoll([key, pubName, poll_id]);
    },
    [canCompletePoll]
  );

  const [key, pubName, poll_id] = completingPoll;
  // Then if we decide to complete, then actually run the complete
  const completeThePoll = useCallback(async () => {
    if (!canCompletePoll) {
      return;
    }
    try {
      await complete_a_poll(key, poll_id);
      setCompletingPoll([]);
    } catch (error) {
      notifyError(getUserFacingErrorMessage(error, "Unable to complete this poll."));
    }
  }, [key, poll_id, canCompletePoll]);

  const completingPollBusy = completingPoll.length !== 0;
  const styleToUse = mobile ? styles.poll_mobile : styles.poll;

  // Change line numbers
  return (
    <div>
      {canCreatePoll && <NewPoll polls={currentPollDates} />}
      <h1>Active Polls</h1>
      {completingPollBusy && (
        <ConfirmModal
          title="Complete this poll?"
          detail={pubName}
          on_confirm={completeThePoll}
          on_cancel={() => {
            setCompletingPoll([]);
          }}
        />
      )}
      {/* js needs to die, and have a very cheap funeral!!! */}
      {[...pollData.sortedByDate()].map(([id, poll]) => (
        <div className={styleToUse} key={id}>
          <ActivePoll
            poll_id={id}
            pub_parameters={pubs}
            poll_data={poll}
            on_complete={completeHandler}
            mobile={mobile}
          />
        </div>
      ))}
    </div>
  );
}
export default ActivePolls;
