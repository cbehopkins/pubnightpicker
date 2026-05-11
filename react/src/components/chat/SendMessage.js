// @ts-check

import React, { useState } from "react";
import { db } from "../../firebase";
import { addDoc, collection, serverTimestamp } from "firebase/firestore";
import styles from "./chat.module.css"
import Button from "../UI/Button";
import { useSelector } from "react-redux";
import { notifyError } from "../../utils/notify";
import useUsers from "../../hooks/useUsers";
import useOnlineStatus from "../../hooks/useOnlineStatus";

/** @typedef {import("../../store").RootState} RootState */

/** @typedef {import('./ChatBox').ChatScope} ChatScope */

/**
 * @param {{ scroll: { current: { scrollIntoView: (options?: ScrollIntoViewOptions) => void } | null }, scope?: ChatScope }} props
 */
const SendMessage = ({ scroll, scope }) => {
  const [message, setMessage] = useState("");
  const users = useUsers();
  const name = useSelector(/** @param {RootState} state */(state) => state.auth.name);
  const uid = useSelector(/** @param {RootState} state */(state) => state.auth.uid);
  // const photoURL = useSelector((state) => state.auth.photoUrl);
  const { isOnline } = useOnlineStatus();

  const sendMessage = async (event) => {
    event.preventDefault();
    if (message.trim() === "") {
      notifyError("Enter valid message");
      return;
    }
    const preferredName = uid ? users?.[uid]?.name : null;
    const messageName = preferredName || name || "Name not set";
    const scopeType = scope?.scopeType ?? "global";
    const scopeId = scope?.scopeId ?? "main";
    await addDoc(collection(db, "messages"), {
      text: message,
      name: messageName,
      createdAt: serverTimestamp(),
      uid,
      scopeType,
      scopeId,
    });
    setMessage("");
    setTimeout(() => { scroll.current?.scrollIntoView({ behavior: "smooth" }); }, 100);
  }
  return (
    <form onSubmit={(event) => sendMessage(event)} className={styles.sendMessage}>
      <label htmlFor="messageInput" hidden>
        Enter Message
      </label>
      <div className="input-group">
        <input
          id="messageInput"
          name="messageInput"
          type="text"
          className="form-control"
          placeholder="Type message..."
          value={message}
          onChange={(e) => setMessage(e.target.value)}
          aria-label="Chat message"
        />
        <Button
          type="submit"
          variant="primary"
          className="px-3"
          disabled={!isOnline}
          title={isOnline ? undefined : "You're offline — messages can't be sent right now"}
        >
          Send
        </Button>
      </div>
    </form>
  );
};

export default SendMessage;
