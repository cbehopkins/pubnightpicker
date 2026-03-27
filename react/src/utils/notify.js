export function notifyInfo(message) {
    if (typeof window !== "undefined" && typeof window.alert === "function") {
        window.alert(message);
        return;
    }

    console.log(message);
}

export function notifyError(message) {
    if (typeof window !== "undefined" && typeof window.alert === "function") {
        window.alert(message);
        return;
    }

    console.error(message);
}
