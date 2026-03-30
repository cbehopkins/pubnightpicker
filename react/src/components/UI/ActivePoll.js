import { useCallback, useState } from "react";
import PollVote from "./PollVote";
import PubOptions from "./PubOptions";
import PubFilter from "./PubFilter";
import styles from "./ActivePoll.module.css";
import useRole from "../../hooks/useRole";
import { AntiPubParams } from "../pages/PubForm"
import { add_new_pub_to_poll, deletePoll } from "../../dbtools/polls"
import { getUserFacingErrorMessage } from "../../permissions";
import { notifyError } from "../../utils/notify";

function anyEntryTrue(anObject) {
  for (const [, value] of Object.entries(anObject)) {
    if (value) {
      return true
    }
    return false
  }
}
function mungePubList(pub_parameters, current_pubs, pubFilters, pubAntiFilters) {
  const allAvailablePubs = new Set(Object.keys(pub_parameters || {}));
  const currentlySelectedPubs = new Set(Object.keys(current_pubs || {}));
  const availablePubs = [...allAvailablePubs].filter((pubId) => {
    // Remove any pubs we've already selected from the list
    if (currentlySelectedPubs.has(pubId)) {
      return false;
    }

    // Remove any pubs with a filter that is set to true
    // Where the pub in question also has that set to true
    const pub_params = pub_parameters[pubId];
    for (const [key, value] of Object.entries(pubFilters)) {
      if (value) {
        if (!(Object.hasOwn(pub_params, key) && pub_params[key])) {
          return false;
        }
      }
    }
    // If there any entries in the anti-filters, then
    if (!anyEntryTrue(pubAntiFilters)) {
      return true
    }
    for (const [key, value] of Object.entries(pubAntiFilters)) {
      if (value) {
        if (Object.hasOwn(pub_params, key) && pub_params[key]) {
          return false;
        }
      }
    }
    return true;
  });
  const newObj = availablePubs.reduce((object, key) => {
    object[key] = pub_parameters[key]
    return object
  }, {})
  return newObj;
}

function ActivePoll({ poll_id, pub_parameters, poll_data, on_complete, mobile }) {
  const canDeletePoll = useRole("canCreatePoll");
  const canAddPub = useRole("canAddPubToPoll");
  const [pubFilters, setPubFilters] = useState({});
  const [pubAntiFilters, setPubAntiFilters] = useState({});


  const [selectedPub, setSelectedPub] = useState("");
  const selectPubHandler = useCallback(
    (event) => {
      event.preventDefault();
      setSelectedPub(event.target.value);
    },
    [setSelectedPub]
  );
  const addNewPubToPoll = useCallback(
    async (event) => {
      event.preventDefault();
      try {
        await add_new_pub_to_poll(selectedPub, poll_id, pub_parameters)
      } catch (error) {
        notifyError(getUserFacingErrorMessage(error, "Unable to add the pub to this poll."));
      }
    },
    [selectedPub, poll_id, pub_parameters]
  );
  const deletePollHandler = useCallback(
    async (event) => {
      event.preventDefault();
      try {
        await deletePoll(poll_id)
      } catch (error) {
        notifyError(getUserFacingErrorMessage(error, "Unable to delete this poll."));
      }
    },
    [poll_id]
  );

  const newObj = mungePubList(pub_parameters, poll_data.pubs, pubFilters, pubAntiFilters)

  const styleToUse = mobile ? styles.poll_mobile : styles.poll;
  const antiParams = AntiPubParams
  return (
    <div className={styleToUse}>
      <h2>{poll_data.date}</h2>
      {canAddPub && <PubFilter title="Filter venue list to contain only items that have:" set_pub_filters={setPubFilters} />}
      {canAddPub && <PubFilter title="Filter venue list to contain only items that do not have:" set_pub_filters={setPubAntiFilters} pub_params={antiParams} />}
      <div>
        {canDeletePoll && (
          <button className={styles["button--alt"]} onClick={deletePollHandler}>
            Delete Poll
          </button>
        )}
        {canAddPub && <>
          <PubOptions
            pub_parameters={newObj}
            selectPubHandler={selectPubHandler}
          />
          <button onClick={addNewPubToPoll}>Add Venue To Poll</button>
        </>
        }
      </div>
      <PollVote
        pub_parameters={pub_parameters}
        poll_data={poll_data}
        poll_id={poll_id}
        on_complete={on_complete}
      />
    </div>
  );
}
export default ActivePoll;
