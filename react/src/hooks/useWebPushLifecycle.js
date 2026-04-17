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
                if (currentStatus.permission !== "default") {
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

                await enableWebPush(uid);
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
            if (!payload || payload.type !== "push-received") {
                return;
            }
            const title = payload.notification?.title || "Notification";
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
