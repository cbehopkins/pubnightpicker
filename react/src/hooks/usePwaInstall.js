import { useEffect, useState } from 'react';

const DISMISSED_KEY = 'pwa-install-dismissed';

function detectIosSafari() {
    const ua = window.navigator.userAgent;
    const isIos = /iphone|ipad|ipod/i.test(ua);
    // Chrome on iOS reports CriOS, Firefox reports FxiOS — neither supports
    // Add to Home Screen the same way as native Safari.
    const isSafari = /safari/i.test(ua) && !/crios|fxios|chrome/i.test(ua);
    return isIos && isSafari;
}

function detectStandalone() {
    const navigatorWithStandalone = /** @type {Navigator & { standalone?: boolean }} */ (
        window.navigator
    );

    return (
        window.matchMedia('(display-mode: standalone)').matches ||
        // Safari-specific property
        navigatorWithStandalone.standalone === true
    );
}

/**
 * Manages the PWA install prompt lifecycle.
 *
 * showInstallButton – true when the browser has an install prompt ready and
 *   the user has not permanently dismissed it on this device.
 *
 * showIosInstructions – true when the user is on iOS Safari (not yet installed)
 *   and has not permanently dismissed the tip on this device.
 *
 * triggerInstall – shows the native browser install dialog.
 *
 * dismiss – records a permanent "not interested" flag in localStorage so the
 *   prompt is never shown again in this browser.
 */
export default function usePwaInstall() {
    const [installPrompt, setInstallPrompt] = useState(null);
    const [dismissed, setDismissed] = useState(
        () => localStorage.getItem(DISMISSED_KEY) === 'true'
    );

    useEffect(() => {
        if (dismissed || detectStandalone()) {
            return undefined;
        }

        const onBeforeInstall = (e) => {
            e.preventDefault();
            setInstallPrompt(e);
        };

        const onInstalled = () => {
            // App was just installed — clear the prompt so the banner hides.
            setInstallPrompt(null);
        };

        window.addEventListener('beforeinstallprompt', onBeforeInstall);
        window.addEventListener('appinstalled', onInstalled);

        return () => {
            window.removeEventListener('beforeinstallprompt', onBeforeInstall);
            window.removeEventListener('appinstalled', onInstalled);
        };
    }, [dismissed]);

    const triggerInstall = async () => {
        if (!installPrompt) return;
        await installPrompt.prompt();
        // Whether they accepted or dismissed the native dialog, clear our copy.
        setInstallPrompt(null);
    };

    const dismiss = () => {
        localStorage.setItem(DISMISSED_KEY, 'true');
        setDismissed(true);
    };

    const standalone = detectStandalone();
    const showInstallButton = !dismissed && !standalone && installPrompt !== null;
    const showIosInstructions = !dismissed && !standalone && detectIosSafari();

    return { showInstallButton, showIosInstructions, triggerInstall, dismiss };
}
