import React, { useEffect, useRef, useState } from "react";
import {
    query,
    collection,
    orderBy,
    onSnapshot,
    limit,
    where,
} from "firebase/firestore";
import { db } from "../../firebase";
import Message from "./Message";
import SendMessage from "./SendMessage";
import useUsers from "../../hooks/useUsers";
import styles from "./chat.module.css"


/** @typedef {{ scopeType: "global" | "event", scopeId: string }} ChatScope */

/** @param {{ scope?: ChatScope }} props */
const ChatBox = ({ scope }) => {
    const [messages, setMessages] = useState([]);
    const users = useUsers();
    const scroll = useRef();

    const scopeType = scope?.scopeType ?? "global";
    const scopeId = scope?.scopeId ?? "main";

    useEffect(() => {
        let q;
        if (scopeType === "event") {
            q = query(
                collection(db, "messages"),
                where("scopeType", "==", "event"),
                where("scopeId", "==", scopeId),
                orderBy("createdAt", "desc"),
                limit(50)
            );
        } else {
            // Global chat: load recent messages ordered by time.
            // Lazy migration: event-scoped messages are excluded client-side
            // until legacy docs are fully backfilled with scope fields.
            q = query(
                collection(db, "messages"),
                orderBy("createdAt", "desc"),
                limit(50)
            );
        }

        const unsubscribe = onSnapshot(q, (QuerySnapshot) => {
            const fetchedMessages = [];
            QuerySnapshot.forEach((doc) => {
                const data = doc.data();
                // Exclude event-scoped messages from the global chat view
                if (scopeType === "global" && data.scopeType === "event") {
                    return;
                }
                fetchedMessages.push({ ...data, id: doc.id });
            });
            const sortedMessages = fetchedMessages.sort(
                (a, b) => a.createdAt - b.createdAt
            );
            setMessages(sortedMessages);
        });
        return unsubscribe;
    }, [scopeType, scopeId]);

    return (<div className={styles.chatBox}>
        <div className={styles.messagesWrapper}>
            <div >
            {messages?.map((message) => (
                <Message key={message.id} message={message} users={users} />
            ))}
        </div>
        {/* when a new message enters the chat, the screen scrolls down to the scroll div */}
        <span ref={scroll}></span>
        </div>
        <div><SendMessage scroll={scroll} scope={scope} /></div>
    </div>
    );
};

export default ChatBox;