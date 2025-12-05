import { useNavigate } from "react-router-dom";
import ChatBox from "../chat/ChatBox";
import { useSelector } from "react-redux";
import { useEffect } from "react";
import useKnown from "../../hooks/useKnown";
import styles from "../chat/chat.module.css"

export default function ChatPage() {
    const loggedIn = useSelector((state) => state.auth.loggedIn);
    const known = useKnown();
    const navigate = useNavigate()
    useEffect(() => {
        if (!loggedIn) {
            navigate("/")
        }
    }, [navigate, loggedIn]);
    return <div className={styles.chatPage}>
        <h1>Chat Page</h1>
        {known && <ChatBox />}
    </div>
}