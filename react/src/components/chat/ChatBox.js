import React, { useEffect, useRef, useState } from "react";
import {
    query,
    collection,
    orderBy,
    onSnapshot,
    limit,
} from "firebase/firestore";
import { db } from "../../firebase";
import Message from "./Message";
import SendMessage from "./SendMessage";
import useUsers from "../../hooks/useUsers";
import "./chat.css"


const ChatBox = () => {
    const [messages, setMessages] = useState([]);
    const users = useUsers();
    const scroll = useRef();

    useEffect(() => {
        const q = query(
            collection(db, "messages"),
            orderBy("createdAt", "desc"),
            limit(50)
        );

        const unsubscribe = onSnapshot(q, (QuerySnapshot) => {
            const fetchedMessages = [];
            QuerySnapshot.forEach((doc) => {
                fetchedMessages.push({ ...doc.data(), id: doc.id });
            });
            const sortedMessages = fetchedMessages.sort(
                (a, b) => a.createdAt - b.createdAt
            );
            setMessages(sortedMessages);
        });
        return unsubscribe;
    }, []);

    return (<div className="chat-box">
        <div className="messages-wrapper">
            <div >
            {messages?.map((message) => (
                <Message key={message.id} message={message} users={users} />
            ))}
        </div>
        {/* when a new message enters the chat, the screen scrolls down to the scroll div */}
        <span ref={scroll}></span>
        </div>
        <div><SendMessage scroll={scroll} className="send-message" /></div>
    </div>
    );
};

export default ChatBox;