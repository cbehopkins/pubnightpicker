// @ts-check

import { useEffect, useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useSelector } from "react-redux";
import { doc, getDoc } from "firebase/firestore";
import ChatBox from "../chat/ChatBox";
import ChatMuteButton from "../chat/ChatMuteButton";
import useRole from "../../hooks/useRole";
import { db } from "../../firebase";
import { setEventChatMuted, setGlobalChatMuted } from "../../push/webPush";
import styles from "../chat/chat.module.css";

/** @typedef {import("../../store").RootState} RootState */

export default function ChatPage() {
    const loggedIn = useSelector(/** @param {RootState} state */(state) => state.auth.loggedIn);
    const uid = useSelector(/** @param {RootState} state */(state) => state.auth.uid);
    const canChat = useRole("canChat");
    const navigate = useNavigate();
    const { pollId } = useParams();
    const [eventChatMuted, setEventChatMutedState] = useState(false);
    const [eventChatMuteBusy, setEventChatMuteBusy] = useState(false);
    const [globalChatMuted, setGlobalChatMutedState] = useState(false);
    const [globalChatMuteBusy, setGlobalChatMuteBusy] = useState(false);
    const [webPushEnabled, setWebPushEnabled] = useState(false);

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

    useEffect(() => {
        const loadEventMuteState = async () => {
            if (!pollId || !uid) {
                setEventChatMutedState(false);
                setWebPushEnabled(false);
                return;
            }
            try {
                const userSnap = await getDoc(doc(db, "users", uid));
                const userData = userSnap.data() || {};
                setWebPushEnabled(userData.webPushEnabled === true);
                const pushPreferences = userData.pushPreferences || {};
                const mutedPollIds = pushPreferences.eventChatMutedPollIds || [];
                setEventChatMutedState(mutedPollIds.includes(pollId));
            } catch {
                setEventChatMutedState(false);
                setWebPushEnabled(false);
            }
        };

        void loadEventMuteState();
    }, [pollId, uid]);

    useEffect(() => {
        const loadGlobalMuteState = async () => {
            if (pollId || !uid) {
                setGlobalChatMutedState(false);
                setWebPushEnabled(false);
                return;
            }
            try {
                const userSnap = await getDoc(doc(db, "users", uid));
                const userData = userSnap.data() || {};
                setWebPushEnabled(userData.webPushEnabled === true);
                const pushPreferences = userData.pushPreferences || {};
                // globalChat: true means enabled; false (or missing) means muted
                setGlobalChatMutedState(pushPreferences.globalChat === false);
            } catch {
                setGlobalChatMutedState(false);
                setWebPushEnabled(false);
            }
        };

        void loadGlobalMuteState();
    }, [pollId, uid]);

    const heading = pollId ? "Event Chat" : "Chat Page";

    const toggleEventMute = async () => {
        if (!pollId || !uid) {
            return;
        }
        setEventChatMuteBusy(true);
        try {
            const nextMuted = !eventChatMuted;
            await setEventChatMuted(uid, pollId, nextMuted);
            setEventChatMutedState(nextMuted);
        } finally {
            setEventChatMuteBusy(false);
        }
    };

    const toggleGlobalMute = async () => {
        if (pollId || !uid) {
            return;
        }
        setGlobalChatMuteBusy(true);
        try {
            const nextMuted = !globalChatMuted;
            await setGlobalChatMuted(uid, nextMuted);
            setGlobalChatMutedState(nextMuted);
        } finally {
            setGlobalChatMuteBusy(false);
        }
    };

    return <div className={styles.chatPage}>
        <div className="d-flex flex-column gap-2 mb-3">
            <div className="d-flex align-items-center justify-content-between gap-2">
                <h1 className="mb-0">{heading}</h1>
                {pollId && webPushEnabled && (
                    <ChatMuteButton
                        muted={eventChatMuted}
                        busy={eventChatMuteBusy}
                        onToggle={() => { void toggleEventMute(); }}
                        label={eventChatMuted ? "Unmute event chat notifications" : "Mute event chat notifications"}
                    />
                )}
                {!pollId && uid && webPushEnabled && (
                    <ChatMuteButton
                        muted={globalChatMuted}
                        busy={globalChatMuteBusy}
                        onToggle={() => { void toggleGlobalMute(); }}
                        label={globalChatMuted ? "Unmute global chat notifications" : "Mute global chat notifications"}
                    />
                )}
            </div>
            {pollId && webPushEnabled && (
                <div className="d-flex flex-column gap-1">
                    <div>
                        <Link to="/current_events">Back to current events</Link>
                    </div>
                    <small className="text-body-secondary">
                        {eventChatMuted
                            ? "Event chat notifications are muted for this event."
                            : "Event chat notifications are enabled for this event."}
                    </small>
                </div>
            )}
            {pollId && !webPushEnabled && (
                <div>
                    <Link to="/current_events">Back to current events</Link>
                </div>
            )}
            {!pollId && uid && webPushEnabled && (
                <small className="text-body-secondary">
                    {globalChatMuted
                        ? "Global chat notifications are muted."
                        : "Global chat notifications are enabled."}
                </small>
            )}
        </div>
        {canChat && <ChatBox scope={chatScope} />}
    </div>;
}
