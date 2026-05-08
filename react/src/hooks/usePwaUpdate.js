import { useEffect, useState } from 'react';

const SW_PATH = '/sw.js';

/**
 * Watches the registered service worker for a waiting update.
 *
 * hasUpdate   – true when a new SW version is installed and waiting to take
 *               over, and the user has not dismissed the banner this session.
 *
 * applyUpdate – posts SKIP_WAITING to the waiting SW, which triggers a
 *               controllerchange event; the hook reloads the page at that point.
 *
 * dismiss     – hides the banner for the rest of this browser session only.
 *               The next fresh load will show it again if the update is still
 *               waiting. This is intentional: we respect the user's choice
 *               in-session but don't permanently block important updates.
 */
export default function usePwaUpdate() {
    const [waitingWorker, setWaitingWorker] = useState(null);
    const [dismissed, setDismissed] = useState(false);

    useEffect(() => {
        if (!('serviceWorker' in navigator)) {
            return undefined;
        }

        let reloadPending = false;

        const onControllerChange = () => {
            // The new SW has taken control. Reload once to use it.
            if (reloadPending) return;
            reloadPending = true;
            window.location.reload();
        };

        navigator.serviceWorker.addEventListener('controllerchange', onControllerChange);

        const watchRegistration = async () => {
            const reg = await navigator.serviceWorker.getRegistration(SW_PATH);
            if (!reg) return;

            // A new SW may already be waiting if this tab was open during a deploy.
            if (reg.waiting && navigator.serviceWorker.controller) {
                setWaitingWorker(reg.waiting);
                return;
            }

            // Listen for future updates discovered during this session.
            reg.addEventListener('updatefound', () => {
                const installing = reg.installing;
                if (!installing) return;

                installing.addEventListener('statechange', () => {
                    if (
                        installing.state === 'installed' &&
                        navigator.serviceWorker.controller
                    ) {
                        setWaitingWorker(installing);
                    }
                });
            });
        };

        void watchRegistration();

        return () => {
            navigator.serviceWorker.removeEventListener('controllerchange', onControllerChange);
        };
    }, []);

    const applyUpdate = () => {
        if (!waitingWorker) return;
        waitingWorker.postMessage({ type: 'SKIP_WAITING' });
    };

    const dismiss = () => setDismissed(true);

    return { hasUpdate: waitingWorker !== null && !dismissed, applyUpdate, dismiss };
}
