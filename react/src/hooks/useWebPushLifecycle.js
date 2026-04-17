import { useEffect, useRef } from "react";
import { doc as firestoreDoc, getDoc } from "firebase/firestore";
import {
    deactivateCurrentWebPushEndpoint,
    enableWebPush,
    hasCurrentWebPushSubscription,
    registerPushServiceWorker,
    touchCurrentWebPushEndpoint,
    webPushStatus,
} from "../push/webPush";
import { db } from "../firebase";
import { notifyInfo } from "../utils/notify";

function showForegroundBrowserNotification(notificationPayload) {
    if (typeof Notification === "undefined" || Notification.permission !== "granted") {
        return false;
    }

    try {
        new Notification(notificationPayload?.title || "Notification", {
            body: notificationPayload?.body || "You have a new notification.",
            tag: notificationPayload?.tag || undefined,
            data: {
                url: notificationPayload?.url || "/",
                eventType: notificationPayload?.eventType || null,
                pollId: notificationPayload?.pollId || null,
            },
        });
        return true;
    } catch {
        return false;
    }
}

export default function useWebPushLifecycle(uid) {
    const previousUidRef = useRef(uid || null);

    useEffect(() => {
        const status = webPushStatus();
        if (!status.featureEnabled || !status.supported) {
            return;
        }

        let cancelled = false;

        const boot = async () => {
            try {
                await registerPushServiceWorker();

                if (!uid) {
                    return;
                }

                await touchCurrentWebPushEndpoint(uid);

                const currentStatus = webPushStatus();
                if (currentStatus.permission === "denied") {
                    return;
                }

                const hasSubscription = await hasCurrentWebPushSubscription();
                if (hasSubscription) {
                    return;
                }

                const userDoc = await getDoc(firestoreDoc(db, "users", uid));
                if (!userDoc.exists()) {
                    return;
                }
                if (userDoc.data()?.webPushEnabled !== true) {
                    return;
                }

                // Re-subscribe this device when opted in but local subscription is missing.
                // If permission is already granted this does not re-prompt the user.
                if (currentStatus.permission === "default" || currentStatus.permission === "granted") {
                    await enableWebPush(uid);
                }
            } catch (err) {
                console.error("Web push lifecycle bootstrap failed", err);
            }
        };

        void boot();

        const handleServiceWorkerMessage = (event) => {
            if (cancelled) {
                return;
            }
            const payload = event?.data;
            if (!payload) {
                return;
            }

            if (payload.type === "push-subscription-changed") {
                if (uid) {
                    void touchCurrentWebPushEndpoint(uid);
                }
                return;
            }

            if (payload.type !== "push-received") {
                return;
            }
            const notificationPayload = payload.notification || {};
            const title = notificationPayload.title || "Notification";

            if (notificationPayload.eventType === "diagnostic_push_test") {
                const shown = showForegroundBrowserNotification(notificationPayload);
                if (!shown) {
                    notifyInfo(title);
                }
                return;
            }

            notifyInfo(title);
        };

        navigator.serviceWorker.addEventListener("message", handleServiceWorkerMessage);

        return () => {
            cancelled = true;
            navigator.serviceWorker.removeEventListener("message", handleServiceWorkerMessage);
        };
    }, [uid]);

    useEffect(() => {
        const previousUid = previousUidRef.current;
        if (previousUid && !uid) {
            void deactivateCurrentWebPushEndpoint(previousUid, { unsubscribe: true });
        }
        previousUidRef.current = uid || null;
    }, [uid]);
}
