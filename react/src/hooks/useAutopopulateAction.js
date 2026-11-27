// @ts-check

import { useCallback, useRef, useState } from "react";
import { autopopulateMeet } from "../dbtools/polls";
import { getUserFacingErrorMessage } from "../permissions";
import { notifyError, notifyInfo } from "../utils/notify";

/** @param {string} pollId */
export default function useAutopopulateAction(pollId) {
    const [isLoading, setIsLoading] = useState(false);
    const pending = useRef(false);
    const handleAutopopulate = useCallback(async () => {
        if (pending.current) return;
        pending.current = true;
        setIsLoading(true);
        try {
            const result = await autopopulateMeet(pollId);
            const additions = Array.isArray(result?.additions) ? result.additions : [];
            if (["aborted", "partial", "uncertain"].includes(result?.outcome)) {
                const count = additions.length;
                const names = additions.map((addition) => addition?.name || addition?.id).filter(Boolean).join(", ");
                const confirmed = names ? `Confirmed: ${names}.` : "No additions were confirmed.";
                const message = result.uncertain
                    ? `The result is uncertain. ${confirmed} No later candidate was attempted.`
                    : `Autopopulate stopped after ${count} confirmed addition${count === 1 ? "" : "s"}. ${confirmed}`;
                notifyError(message);
            } else if (additions.length === 0) {
                notifyInfo("No candidates were available to add.");
            } else {
                notifyInfo(`Added ${additions.length} venue${additions.length === 1 ? "" : "s"} to the poll${result?.exhausted ? "; no more candidates were available." : "."}`);
            }
        } catch (error) {
            notifyError(getUserFacingErrorMessage(error, "Unable to autopopulate venues."));
        } finally {
            pending.current = false;
            setIsLoading(false);
        }
    }, [pollId]);

    return { handleAutopopulate, isLoading };
}
