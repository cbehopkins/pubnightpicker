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
    const { meetId } = useParams();
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
        if (!meetId) {
            return undefined;
        }
        return { scopeType: /** @type {"event"} */ ("event"), scopeId: meetId };
    }, [meetId]);

    useEffect(() => {
        const loadEventMuteState = async () => {
            if (!meetId || !uid) {
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
                setEventChatMutedState(mutedPollIds.includes(meetId));
            } catch {
                setEventChatMutedState(false);
                setWebPushEnabled(false);
            }
        };

        void loadEventMuteState();
    }, [meetId, uid]);

    useEffect(() => {
        const loadGlobalMuteState = async () => {
            if (meetId || !uid) {
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
    }, [meetId, uid]);

    const heading = meetId ? "Meet Chat" : "Chat Page";

    const toggleEventMute = async () => {
        if (!meetId || !uid) {
            return;
        }
        setEventChatMuteBusy(true);
        try {
            const nextMuted = !eventChatMuted;
            await setEventChatMuted(uid, meetId, nextMuted);
            setEventChatMutedState(nextMuted);
        } finally {
            setEventChatMuteBusy(false);
        }
    };

    const toggleGlobalMute = async () => {
        if (meetId || !uid) {
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
                {meetId && webPushEnabled && (
                    <ChatMuteButton
                        muted={eventChatMuted}
                        busy={eventChatMuteBusy}
                        onToggle={() => { void toggleEventMute(); }}
                        label={eventChatMuted ? "Unmute Meet chat notifications" : "Mute Meet chat notifications"}
                    />
                )}
                {!meetId && uid && webPushEnabled && (
                    <ChatMuteButton
                        muted={globalChatMuted}
                        busy={globalChatMuteBusy}
                        onToggle={() => { void toggleGlobalMute(); }}
                        label={globalChatMuted ? "Unmute global chat notifications" : "Mute global chat notifications"}
                    />
                )}
            </div>
            {meetId && webPushEnabled && (
                <div className="d-flex flex-column gap-1">
                    <div>
                        <Link to="/current_meets">Back to current meets</Link>
                    </div>
                    <small className="text-body-secondary">
                        {eventChatMuted
                            ? "Meet chat notifications are muted for this Meet."
                            : "Meet chat notifications are enabled for this Meet."}
                    </small>
                </div>
            )}
            {meetId && !webPushEnabled && (
                <div>
                    <Link to="/current_meets">Back to current meets</Link>
                </div>
            )}
            {!meetId && uid && webPushEnabled && (
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
