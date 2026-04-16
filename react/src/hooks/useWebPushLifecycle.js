import { useEffect, useRef } from "react";
import {
    deactivateCurrentWebPushEndpoint,
    registerPushServiceWorker,
    touchCurrentWebPushEndpoint,
    webPushStatus,
} from "../push/webPush";
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
            await registerPushServiceWorker();

            if (uid) {
                await touchCurrentWebPushEndpoint(uid);
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
