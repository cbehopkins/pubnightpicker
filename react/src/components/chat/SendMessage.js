import React, { useState } from "react";
import { db } from "../../firebase";
import { addDoc, collection, serverTimestamp } from "firebase/firestore";
import styles from "./chat.module.css"
import { useSelector } from "react-redux";
import { notifyError } from "../../utils/notify";

const SendMessage = ({ scroll }) => {
  const [message, setMessage] = useState("");
  const name = useSelector((state) => state.auth.name);
  const uid = useSelector((state) => state.auth.uid);
  // const photoURL = useSelector((state) => state.auth.photoUrl);

  const sendMessage = async (event) => {
    event.preventDefault();
    if (message.trim() === "") {
      notifyError("Enter valid message");
      return;
    }
    await addDoc(collection(db, "messages"), {
      text: message,
      name: name,
      createdAt: serverTimestamp(),
      uid,
    });
    setMessage("");
    setTimeout(() => { scroll.current.scrollIntoView({ behavior: "smooth" }); }, 100);
  }
  return (
    <form onSubmit={(event) => sendMessage(event)} className={styles.sendMessage}>
      <label htmlFor="messageInput" hidden>
        Enter Message
      </label>
      <input
        id="messageInput"
        name="messageInput"
        type="text"
        className="form-input__input"
        placeholder="type message..."
        value={message}
        onChange={(e) => setMessage(e.target.value)}
      />
      <button type="submit">Send</button>
    </form>
  );
};

export default SendMessage;
