// @ts-check

import { useNavigate } from "react-router-dom";
import ChatBox from "../chat/ChatBox";
import { useSelector } from "react-redux";
import { useEffect } from "react";
import useRole from "../../hooks/useRole";
import styles from "../chat/chat.module.css"

/** @typedef {import("../../store").RootState} RootState */

export default function ChatPage() {
    const loggedIn = useSelector(/** @param {RootState} state */(state) => state.auth.loggedIn);
    const canChat = useRole("canChat");
    const navigate = useNavigate()
    useEffect(() => {
        if (!loggedIn) {
            navigate("/")
        }
    }, [navigate, loggedIn]);
    return <div className={styles.chatPage}>
        <h1>Chat Page</h1>
        {canChat && <ChatBox />}
    </div>
}
