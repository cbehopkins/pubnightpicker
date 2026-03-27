import { notifyError } from "./notify";

const SNAPSHOT_ERROR_COOLDOWN_MS = 15000;
const lastShownByKey = new Map();

function getFriendlyFirestoreMessage(error, contextLabel) {
    const code = error?.code;

    if (code === "unavailable") {
        return `${contextLabel}: Cannot reach Firestore. Check the emulator or network connection.`;
    }

    if (code === "permission-denied") {
        return `${contextLabel}: Permission denied while loading data.`;
    }

    if (error?.message) {
        return `${contextLabel}: ${error.message}`;
    }

    return `${contextLabel}: Unable to load live updates from Firestore.`;
}

export function createFirestoreSnapshotErrorHandler(contextLabel = "Live data") {
    return (error) => {
        const key = `${contextLabel}:${error?.code || "unknown"}`;
        const now = Date.now();
        const lastShown = lastShownByKey.get(key) || 0;

        if (now - lastShown < SNAPSHOT_ERROR_COOLDOWN_MS) {
            return;
        }

        lastShownByKey.set(key, now);
        notifyError(getFriendlyFirestoreMessage(error, contextLabel));
    };
}
