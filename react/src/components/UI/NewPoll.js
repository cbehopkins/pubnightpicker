import React, { useState } from "react";

import { doc, collection, addDoc, setDoc } from "firebase/firestore";
import { db } from "../../firebase";
import Button from "../UI/Button";

function NewPoll(params) {
  const [enteredDate, setEnteredDate] = useState("");
  const dateChangeHandler = (event) => {
    setEnteredDate(event.target.value);
  };
  const addNewPollHandler = async (event) => {
    event.preventDefault();

    try {
      const docRef = await addDoc(collection(db, "polls"), {
        date: enteredDate,
        completed: false,
      });
      setEnteredDate("");
      // This is important to do here as the permissions should be set on the database
      // Such that only those who create polls can write vs update
      await setDoc(doc(db, "votes", docRef.id), {any:[]})
    } catch (err) {
      console.error("Error adding document: ", err);
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
        min={new Date()}
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
