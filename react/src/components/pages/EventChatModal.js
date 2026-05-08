// @ts-check

import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { useSelector } from "react-redux";
import { doc, getDoc } from "firebase/firestore";
import Modal from "../UI/Modal";
import ChatBox from "../chat/ChatBox";
import ChatMuteButton from "../chat/ChatMuteButton";
import Button from "../UI/Button";
import { db } from "../../firebase";
import { setEventChatMuted } from "../../push/webPush";

/** @typedef {import("../../store").RootState} RootState */

/**
 * @param {{ pollId: string, onClose: () => void }} props
 */
export default function EventChatModal({ pollId, onClose }) {
    const uid = useSelector(/** @param {RootState} state */(state) => state.auth.uid);
    const scope = useMemo(
        () => ({ scopeType: /** @type {"event"} */ ("event"), scopeId: pollId }),
        [pollId]
    );
    const [eventChatMuted, setEventChatMutedState] = useState(false);
    const [eventChatMuteBusy, setEventChatMuteBusy] = useState(false);

    useEffect(() => {
        const loadMuteState = async () => {
            if (!pollId || !uid) {
                setEventChatMutedState(false);
                return;
            }
            try {
                const userSnap = await getDoc(doc(db, "users", uid));
                const userData = userSnap.data() || {};
                const pushPreferences = userData.pushPreferences || {};
                const mutedPollIds = pushPreferences.eventChatMutedPollIds || [];
                setEventChatMutedState(mutedPollIds.includes(pollId));
            } catch {
                setEventChatMutedState(false);
            }
        };

        void loadMuteState();
    }, [pollId, uid]);

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

    return (
        <Modal onBackdropClick={onClose}>
            <div className="d-flex flex-column gap-2">
                <div className="d-flex align-items-center justify-content-between gap-2">
                    <h2 className="h5 mb-0">
                        <Link to={`/chat/event/${pollId}`} onClick={onClose}>Event Chat</Link>
                    </h2>
                    {uid && (
                        <ChatMuteButton
                            muted={eventChatMuted}
                            busy={eventChatMuteBusy}
                            onToggle={() => { void toggleEventMute(); }}
                            label={eventChatMuted ? "Unmute event chat notifications" : "Mute event chat notifications"}
                        />
                    )}
                    <Button
                        type="button"
                        variant="secondary"
                        onClick={onClose}
                        aria-label="Close event chat"
                    >
                        Close
                    </Button>
                </div>
                {uid && (
                    <small className="text-body-secondary">
                        {eventChatMuted
                            ? "Event chat notifications are muted for this event."
                            : "Event chat notifications are enabled for this event."}
                    </small>
                )}
                <ChatBox scope={scope} />
            </div>
        </Modal>
    );
}
