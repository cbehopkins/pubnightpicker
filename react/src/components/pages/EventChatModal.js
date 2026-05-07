// @ts-check

import { useMemo } from "react";
import Modal from "../UI/Modal";
import ChatBox from "../chat/ChatBox";
import Button from "../UI/Button";

/**
 * @param {{ pollId: string, onClose: () => void }} props
 */
export default function EventChatModal({ pollId, onClose }) {
    const scope = useMemo(
        () => ({ scopeType: /** @type {"event"} */ ("event"), scopeId: pollId }),
        [pollId]
    );

    return (
        <Modal onBackdropClick={onClose}>
            <div className="d-flex flex-column gap-2">
                <div className="d-flex align-items-center justify-content-between">
                    <h2 className="h5 mb-0">Event Chat</h2>
                    <Button
                        type="button"
                        variant="secondary"
                        onClick={onClose}
                        aria-label="Close event chat"
                    >
                        Close
                    </Button>
                </div>
                <ChatBox scope={scope} />
            </div>
        </Modal>
    );
}
