import { useNavigate } from "react-router-dom";
import ChatBox from "../chat/ChatBox";
import { useSelector } from "react-redux";
import { useEffect } from "react";
import useKnown from "../../hooks/useKnown";
import "../chat/chat.css"

export default function ChatPage() {
    const loggedIn = useSelector((state) => state.auth.loggedIn);
    const known = useKnown();
    const navigate = useNavigate()
    useEffect(() => {
        if (!loggedIn) {
            navigate("/")
        }
    }, [navigate, loggedIn]);
    return <div className="chat-page">
        <h1>Chat Page</h1>
        {known && <ChatBox />}
    </div>
}