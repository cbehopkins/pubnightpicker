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
        <div className={`d-flex align-items-start gap-2 mb-3 ${messageFromMe ? "justify-content-end" : ""}`}>
            {!messageFromMe && (
                <img
                    className={styles.chatBubbleLeft}
                    src={avatar}
                    alt="user avatar"
                    referrerPolicy="no-referrer"
                />
            )}

            <div className={`card shadow-sm ${styles.messageCard} ${messageFromMe ? "bg-primary-subtle border-primary-subtle" : "bg-body-tertiary border"}`}>
                <div className="card-body p-2 p-md-3">
                    <div className="d-flex align-items-start justify-content-between gap-2">
                        <p className={`${styles.userName} text-body-emphasis mb-1`}>{name}</p>
                        {deleteAllowed && (
                            <button
                                className="btn btn-sm btn-link text-danger p-0"
                                onClick={deleteMessage}
                                type="button"
                                aria-label="Delete Message"
                                title="Delete Message"
                            >
                                <img src={DeleteIcon} alt="" aria-hidden="true" />
                            </button>
                        )}
                    </div>
                    <p className={`${styles.userMessage} text-body mb-0`}>{message.text}</p>
                </div>
            </div>

            {messageFromMe && (
                <img
                    className={styles.chatBubbleLeft}
                    src={avatar}
                    alt="user avatar"
                    referrerPolicy="no-referrer"
                />
            )}
        </div>
    );
};

export default Message;
