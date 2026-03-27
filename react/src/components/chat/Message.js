import React from "react";
import { useSelector } from "react-redux";
import { doc, deleteDoc } from "firebase/firestore";
import { db } from "../../firebase";
import UnknownUser from "../../img/unknown_user.png";
import DeleteIcon from "../../img/x-close-delete.svg";
import useRole from "../../hooks/useRole";
import { getUserFacingErrorMessage } from "../../permissions";
import { notifyError } from "../../utils/notify";
import styles from "./chat.module.css"

const Message = ({ message, users }) => {
    const canDeleteAnyMessage = useRole("canDeleteAnyMessage");
    const uid = useSelector((state) => state.auth.uid);
    const avatar = users[message.uid]?.photoUrl || UnknownUser;
    const name = message.name || "Name not set"
    const messageFromMe = message.uid === uid
    const deleteAllowed = canDeleteAnyMessage || messageFromMe
    const deleteMessage = async () => {
        if (!deleteAllowed) {
            return
        }
        try {
            await deleteDoc(doc(db, "messages", message.id));
        } catch (error) {
            notifyError(getUserFacingErrorMessage(error, "Unable to delete this message."));
        }
    }

    return (
        <div className={`${styles.chatBubble} ${messageFromMe ? styles.right : ""}`}>
            <img
                className={styles.chatBubbleLeft}
                src={avatar}
                alt="user avatar"
                referrerPolicy="no-referrer"
            />
            <div className="chat-bubble__right">
                <p className={styles.userName}>{name}</p>
                <p className={styles.userMessage}>{message.text}</p>
            </div>
            {deleteAllowed && <img src={DeleteIcon} onClick={deleteMessage} alt="Delete Message" />}
        </div>
    );
};

export default Message;
