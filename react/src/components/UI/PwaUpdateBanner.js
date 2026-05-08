import usePwaUpdate from '../../hooks/usePwaUpdate';

/**
 * Shown when a new version of the app has downloaded in the background and
 * is ready to activate.
 *
 * "Refresh" activates it immediately. Dismissing hides it for this session
 * only — it reappears next time the user opens the app if they still haven't
 * updated.
 */
export default function PwaUpdateBanner() {
    const { hasUpdate, applyUpdate, dismiss } = usePwaUpdate();

    if (!hasUpdate) {
        return null;
    }

    return (
        <div
            className="alert alert-warning alert-dismissible d-flex align-items-center gap-2 m-0 rounded-0 border-0 border-bottom py-2 px-3"
            role="alert"
        >
            <span className="flex-grow-1 small">
                A new version of Pub Night Picker is available.
            </span>

            <button
                type="button"
                className="btn btn-sm btn-outline-dark"
                onClick={applyUpdate}
            >
                Refresh
            </button>

            <button
                type="button"
                className="btn-close"
                aria-label="Dismiss"
                onClick={dismiss}
            />
        </div>
    );
}
