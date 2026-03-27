import { useCallback, useEffect, useState } from "react";
import { NOTIFY_EVENT_NAME } from "../../utils/notify";
import styles from "./ToastCenter.module.css";

const DEFAULT_TIMEOUT_MS = 4500;

export default function ToastCenter() {
    const [toasts, setToasts] = useState([]);

    const removeToast = useCallback((id) => {
        setToasts((current) => current.filter((toast) => toast.id !== id));
    }, []);

    useEffect(() => {
        const onNotify = (event) => {
            const detail = event?.detail;
            if (!detail || !detail.message) {
                return;
            }

            const toast = {
                id: detail.id || `${Date.now()}-${Math.random()}`,
                message: detail.message,
                level: detail.level || "info",
            };

            setToasts((current) => [...current, toast]);

            window.setTimeout(() => {
                removeToast(toast.id);
            }, DEFAULT_TIMEOUT_MS);
        };

        window.addEventListener(NOTIFY_EVENT_NAME, onNotify);
        return () => {
            window.removeEventListener(NOTIFY_EVENT_NAME, onNotify);
        };
    }, [removeToast]);

    if (toasts.length === 0) {
        return null;
    }

    return (
        <div className={styles.stack} aria-live="polite" aria-atomic="false">
            {toasts.map((toast) => {
                const levelClass = toast.level === "error" ? styles.error : styles.info;
                return (
                    <div key={toast.id} className={`${styles.toast} ${levelClass}`}>
                        <span className={styles.message}>{toast.message}</span>
                        <button
                            className={styles.dismiss}
                            onClick={() => removeToast(toast.id)}
                            aria-label="Dismiss notification"
                            type="button"
                        >
                            x
                        </button>
                    </div>
                );
            })}
        </div>
    );
}
