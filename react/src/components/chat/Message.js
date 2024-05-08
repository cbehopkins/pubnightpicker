import React from "react";
import { useSelector } from "react-redux";
import { doc, deleteDoc } from "firebase/firestore";
import { db } from "../../firebase";
import UnknownUser from "../../img/unknown_user.png";
import DeleteIcon from "../../img/x-close-delete.svg";
import useAdmin from "../../hooks/useAdmin";

import "./chat.css"
const Message = ({ message, users }) => {
    const admin = useAdmin();
    const uid = useSelector((state) => state.auth.uid);
    const avatar = users[message.uid]?.photoUrl || UnknownUser;
    const name = message.name || "Name not set"
    const messageFromMe = message.uid === uid
    const deleteAllowed = admin || messageFromMe
    const deleteMessage = async () => {
        if (!deleteAllowed) {
            return
        }
        await deleteDoc(doc(db, "messages", message.id));
    }

    return (
        <div className={`chat-bubble ${messageFromMe ? "right" : ""}`}>
            <img
                className="chat-bubble__left"
                src={avatar}
                alt="user avatar"
                referrerPolicy="no-referrer"
            />
            <div className="chat-bubble__right">
                <p className="user-name">{name}</p>
                <p className="user-message">{message.text}</p>
            </div>
            {deleteAllowed && <img src={DeleteIcon} onClick={deleteMessage} alt="Delete Message" />}
        </div>
    );
};

export default Message;