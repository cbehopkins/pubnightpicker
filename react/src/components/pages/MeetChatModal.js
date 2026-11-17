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
 * @param {{ meetId: string, onClose: () => void }} props
 */
export default function MeetChatModal({ meetId, onClose }) {
    const uid = useSelector(/** @param {RootState} state */(state) => state.auth.uid);
    const scope = useMemo(
        () => ({ scopeType: /** @type {"event"} */ ("event"), scopeId: meetId }),
        [meetId]
    );
    const [meetChatMuted, setMeetChatMuted] = useState(false);
    const [meetChatMuteBusy, setMeetChatMuteBusy] = useState(false);
    const [webPushEnabled, setWebPushEnabled] = useState(false);

    useEffect(() => {
        const loadMuteState = async () => {
            if (!meetId || !uid) {
                setMeetChatMuted(false);
                return;
            }
            try {
                const userSnap = await getDoc(doc(db, "users", uid));
                const userData = userSnap.data() || {};
                setWebPushEnabled(userData.webPushEnabled === true);
                const pushPreferences = userData.pushPreferences || {};
                const mutedMeetIds = pushPreferences.eventChatMutedPollIds || [];
                setMeetChatMuted(mutedMeetIds.includes(meetId));
            } catch {
                setMeetChatMuted(false);
                setWebPushEnabled(false);
            }
        };

        void loadMuteState();
    }, [meetId, uid]);

    const toggleMeetMute = async () => {
        if (!meetId || !uid) {
            return;
        }
        setMeetChatMuteBusy(true);
        try {
            const nextMuted = !meetChatMuted;
            await setEventChatMuted(uid, meetId, nextMuted);
            setMeetChatMuted(nextMuted);
        } finally {
            setMeetChatMuteBusy(false);
        }
    };

    return (
        <Modal onBackdropClick={onClose}>
            <div className="d-flex flex-column gap-2">
                <div className="d-flex align-items-center justify-content-between gap-2">
                    <h2 className="h5 mb-0">
                        <Link to={`/chat/meet/${meetId}`} onClick={onClose}>Meet Chat</Link>
                    </h2>
                    {uid && webPushEnabled && (
                        <ChatMuteButton
                            muted={meetChatMuted}
                            busy={meetChatMuteBusy}
                            onToggle={() => { void toggleMeetMute(); }}
                            label={meetChatMuted ? "Unmute Meet chat notifications" : "Mute Meet chat notifications"}
                        />
                    )}
                    <Button
                        type="button"
                        variant="secondary"
                        onClick={onClose}
                        aria-label="Close Meet chat"
                    >
                        Close
                    </Button>
                </div>
                {uid && webPushEnabled && (
                    <small className="text-body-secondary">
                        {meetChatMuted
                            ? "Meet chat notifications are muted for this Meet."
                            : "Meet chat notifications are enabled for this Meet."}
                    </small>
                )}
                <ChatBox scope={scope} />
            </div>
        </Modal>
    );
}
