import { useCallback, useEffect, useState } from "react";
import {
    deactivateCurrentWebPushEndpoint,
    enableWebPush,
    hasCurrentWebPushSubscription,
    setWebPushPreference,
    webPushStatus,
} from "../push/webPush";

export default function useWebPushSettings(uid, initialEnabled = false) {
    const status = webPushStatus();
    const [busy, setBusy] = useState(false);
    const [enabled, setEnabled] = useState(Boolean(initialEnabled));
    const [error, setError] = useState("");

    useEffect(() => {
        let cancelled = false;

        const syncLocalEnabledState = async () => {
            if (!uid) {
                setEnabled(false);
                return;
            }
            try {
                const hasSubscription = await hasCurrentWebPushSubscription();
                if (!cancelled) {
                    setEnabled(hasSubscription);
                }

                if (hasSubscription && !initialEnabled) {
                    await setWebPushPreference(uid, true);
                }
            } catch {
                if (!cancelled) {
                    setEnabled(false);
                }
            }
        };

        void syncLocalEnabledState();

        return () => {
            cancelled = true;
        };
    }, [initialEnabled, uid]);

    const enable = useCallback(async () => {
        if (!uid) {
            setError("You must be logged in to enable web push");
            return false;
        }
        setBusy(true);
        setError("");
        try {
            await enableWebPush(uid);
            setEnabled(true);
            return true;
        } catch (err) {
            setError(err?.message || "Unable to enable web push");
            return false;
        } finally {
            setBusy(false);
        }
    }, [uid]);

    const disable = useCallback(async () => {
        if (!uid) {
            setError("You must be logged in to disable web push");
            return false;
        }
        setBusy(true);
        setError("");
        try {
            await deactivateCurrentWebPushEndpoint(uid, { unsubscribe: true });
            setEnabled(false);
            return true;
        } catch (err) {
            setError(err?.message || "Unable to disable web push");
            return false;
        } finally {
            setBusy(false);
        }
    }, [uid]);

    return {
        busy,
        enable,
        disable,
        enabled,
        error,
        supported: status.supported,
        featureEnabled: status.featureEnabled,
        permission: status.permission,
    };
}
