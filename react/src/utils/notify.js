export const NOTIFY_EVENT_NAME = "app:notify";

function emitNotification(level, message) {
    if (
        typeof window !== "undefined"
        && typeof window.dispatchEvent === "function"
        && typeof CustomEvent !== "undefined"
    ) {
        window.dispatchEvent(new CustomEvent(NOTIFY_EVENT_NAME, {
            detail: {
                id: `${Date.now()}-${Math.random()}`,
                level,
                message,
            },
        }));
        return;
    }

    if (level === "error") {
        console.error(message);
        return;
    }
    console.log(message);
}

export function notifyInfo(message) {
    emitNotification("info", message);
}

export function notifyError(message) {
    emitNotification("error", message);
}
