import useOnlineStatus from '../../hooks/useOnlineStatus';

/**
 * Shown automatically when the browser loses connectivity.
 * Disappears automatically when it comes back.
 * Not dismissible — it reflects factual device state, not a prompt.
 */
export default function OfflineBanner() {
    const { isOnline } = useOnlineStatus();

    if (isOnline) {
        return null;
    }

    return (
        <div
            className="alert alert-secondary d-flex align-items-center gap-2 m-0 rounded-0 border-0 border-bottom py-2 px-3"
            role="status"
            aria-live="polite"
        >
            <span className="flex-grow-1 small">
                You're offline. Votes and attendance changes will sync when you reconnect.
            </span>
        </div>
    );
}
