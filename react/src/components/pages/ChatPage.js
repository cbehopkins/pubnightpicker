// @ts-check

import { useEffect, useMemo } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useSelector } from "react-redux";
import ChatBox from "../chat/ChatBox";
import useRole from "../../hooks/useRole";
import styles from "../chat/chat.module.css";

/** @typedef {import("../../store").RootState} RootState */

export default function ChatPage() {
    const loggedIn = useSelector(/** @param {RootState} state */(state) => state.auth.loggedIn);
    const canChat = useRole("canChat");
    const navigate = useNavigate();
    const { pollId } = useParams();

    useEffect(() => {
        if (!loggedIn) {
            navigate("/");
        }
    }, [navigate, loggedIn]);

    const chatScope = useMemo(() => {
        if (!pollId) {
            return undefined;
        }
        return { scopeType: /** @type {"event"} */ ("event"), scopeId: pollId };
    }, [pollId]);

    const heading = pollId ? "Event Chat" : "Chat Page";

    return <div className={styles.chatPage}>
        <div className="d-flex flex-column gap-2 mb-3">
            <h1 className="mb-0">{heading}</h1>
            {pollId && (
                <div>
                    <Link to="/current_events">Back to current events</Link>
                </div>
            )}
        </div>
        {canChat && <ChatBox scope={chatScope} />}
    </div>;
}
