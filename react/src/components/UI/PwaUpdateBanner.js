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
            className="pwa-update-banner alert alert-warning m-0 rounded-0 border-0 border-bottom py-2 px-3"
            role="alert"
        >
            <div className="d-flex flex-column flex-sm-row align-items-start align-items-sm-center gap-2 w-100">
                <span className="small">
                    A new version of Pub Night Picker is available.
                </span>

                <div className="d-flex align-items-center gap-2 ms-sm-auto">
                    <button
                        type="button"
                        className="btn btn-sm pwa-update-banner__refresh"
                        onClick={applyUpdate}
                    >
                        Refresh
                    </button>

                    <button
                        type="button"
                        className="btn-close pwa-update-banner__dismiss"
                        aria-label="Dismiss"
                        onClick={dismiss}
                    />
                </div>
            </div>
        </div>
    );
}
