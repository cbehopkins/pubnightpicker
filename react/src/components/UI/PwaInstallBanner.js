import usePwaInstall from '../../hooks/usePwaInstall';

/**
 * Shown once per device/browser when the app can be installed.
 * Permanently dismissed via localStorage — never shown again on this device
 * once the user closes it.
 */
export default function PwaInstallBanner() {
    const { showInstallButton, showIosInstructions, triggerInstall, dismiss } =
        usePwaInstall();

    if (!showInstallButton && !showIosInstructions) {
        return null;
    }

    return (
        <div
            className="alert alert-info alert-dismissible d-flex align-items-center gap-2 m-0 rounded-0 border-0 border-bottom py-2 px-3"
            role="alert"
        >
            <span className="flex-grow-1 small">
                {showIosInstructions
                    ? 'Install this app: tap the Share button in Safari, then "Add to Home Screen".'
                    : 'Install Pub Night Picker as an app for the best experience.'}
            </span>

            {showInstallButton && (
                <button
                    type="button"
                    className="btn btn-sm btn-outline-primary"
                    onClick={triggerInstall}
                >
                    Install
                </button>
            )}

            <button
                type="button"
                className="btn-close"
                aria-label="Dismiss"
                onClick={dismiss}
            />
        </div>
    );
}
