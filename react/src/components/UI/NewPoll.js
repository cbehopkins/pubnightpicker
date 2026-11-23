// @ts-check

import { useState } from "react";
import { useSelector } from "react-redux";

import Button from "../UI/Button";
import { requestLastOrders } from "../../utils/lastOrdersApi";
import { getUserFacingErrorMessage } from "../../permissions";
import { notifyError } from "../../utils/notify";

/** @typedef {{ polls: Set<string> }} NewPollProps */

/**
 * @param {NewPollProps} params
 */
function NewPoll(params) {
  const groupId = useSelector(
    /** @param {import("../../store").RootState} state */
    (state) => state.group.groupId,
  );
  const [enteredDate, setEnteredDate] = useState("");
  const minDate = new Date().toISOString().slice(0, 10);

  /** @param {React.ChangeEvent<HTMLInputElement>} event */
  const dateChangeHandler = (event) => {
    setEnteredDate(event.target.value);
  };

  /** @param {React.MouseEvent<HTMLButtonElement>} event */
  const addNewPollHandler = async (event) => {
    event.preventDefault();

    try {
      if (!groupId) throw new Error("No resolved Group for poll creation.");
      await requestLastOrders(`api/meets/${encodeURIComponent(groupId)}`, {
        method: "POST",
        body: { date: enteredDate },
      });
      setEnteredDate("");
    } catch (err) {
      const uncertain = err?.code === "connection_error"
        || err?.code === "internal_error"
        || err?.code === "backend_error"
        || err?.code === "invalid_response";
      notifyError(uncertain
        ? "Meet creation outcome is uncertain. Refresh the open Meets view before retrying."
        : getUserFacingErrorMessage(err, "Unable to create this meet."));
    }
  };
  const validateDate = () => {
    if (!enteredDate) {
      return false;
    }
    if (params.polls.has(enteredDate)) {
      // FIXME do a popup here
      // console.error(enteredDate, "is already an active poll");
      return false;
    }

    return true;
  };
  const dateValid = validateDate();
  return (
    <>
      <h1>Add Poll</h1>
      <label>Date</label>
      <input
        type="date"
        min={minDate}
        value={enteredDate}
        onChange={dateChangeHandler}
      />
      <Button disabled={!dateValid} onClick={addNewPollHandler}>
        Add Poll
      </Button>
    </>
  );
}
export default NewPoll;
