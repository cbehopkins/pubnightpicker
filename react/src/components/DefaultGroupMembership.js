import { useEffect, useState } from "react";
import { requestLastOrders } from "../utils/lastOrdersApi";

/** @param {{ groupId: string, uid: string | null, loading: boolean, children: import("react").ReactNode }} props */
export default function DefaultGroupMembership({ groupId, uid, loading, children }) {
    const [attempt, setAttempt] = useState(0);
    const [status, setStatus] = useState("loading");

    useEffect(() => {
        if (loading || !uid) return;
        let active = true;
        const controller = new AbortController();
        const timeout = setTimeout(() => controller.abort(), 10000);
        requestLastOrders(`/api/groups/${encodeURIComponent(groupId)}/members/${encodeURIComponent(uid)}`, {
            method: "PUT",
            signal: controller.signal,
        }).then(() => {
            if (active) setStatus("ready");
        }).catch(() => {
            if (active) setStatus("error");
        }).finally(() => clearTimeout(timeout));
        return () => {
            active = false;
            clearTimeout(timeout);
            controller.abort();
        };
    }, [attempt, groupId, loading, uid]);

    if (!loading && (!uid || status === "ready")) return children;

    return <main className="container min-vh-100 d-flex flex-column align-items-center justify-content-center py-4">
        {status === "error" ? (
            <div className="alert alert-danger mb-0" role="alert">
                <p className="mb-3">Unable to join the default Group. Check the backend connection and try again.</p>
                <button type="button" className="btn btn-secondary" onClick={() => {
                    setStatus("loading");
                    setAttempt((value) => value + 1);
                }}>Retry</button>
            </div>
        ) : <p role="status" aria-live="polite" className="mb-0">Preparing your Group membership...</p>}
    </main>;
}
