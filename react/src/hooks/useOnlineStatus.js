import { useEffect, useState } from 'react';

/**
 * Reflects the browser's current network connectivity.
 * Updates in real time when the browser goes online or offline.
 *
 * Note: `navigator.onLine` is optimistic — it reports true whenever the
 * device has *any* network interface active, even if that interface has no
 * route to the internet. For a local app like this the signal is accurate
 * enough: it will be false whenever the device has no connectivity at all,
 * which is the main case we want to communicate to the user.
 */
export default function useOnlineStatus() {
    const [isOnline, setIsOnline] = useState(() => navigator.onLine);

    useEffect(() => {
        const handleOnline = () => setIsOnline(true);
        const handleOffline = () => setIsOnline(false);

        window.addEventListener('online', handleOnline);
        window.addEventListener('offline', handleOffline);

        return () => {
            window.removeEventListener('online', handleOnline);
            window.removeEventListener('offline', handleOffline);
        };
    }, []);

    return { isOnline };
}
