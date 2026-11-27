import { requestLastOrders } from "./lastOrdersApi";

/**
 * @param {string} _uid Kept for existing callers; the API derives identity from the verified token.
 */
export async function buildCurrentUserDataExport(_uid) {
    const payload = await requestLastOrders("/api/account/export", { method: "GET" });
    if (!payload || typeof payload !== "object" || typeof payload.user_id !== "string") {
        throw new Error("Last Orders returned an unexpected account export.");
    }
    return payload;
}

/** @param {string} filename @param {unknown} payload */
export function downloadJson(filename, payload) {
    const blob = new Blob([JSON.stringify(payload, null, 2)], { type: "application/json" });
    const objectUrl = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = objectUrl;
    link.download = filename;
    document.body.appendChild(link);
    link.click();
    link.remove();
    URL.revokeObjectURL(objectUrl);
}

/** @param {string} uid */
export async function downloadCurrentUserDataExport(uid) {
    const payload = await buildCurrentUserDataExport(uid);
    downloadJson("pub-night-picker-data.json", payload);
    return payload;
}
