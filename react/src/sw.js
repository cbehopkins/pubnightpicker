// =============================================================================
// Pub Night Picker — Service Worker
// =============================================================================
// This file is compiled by vite-plugin-pwa using Rollup, so it can use
// standard ES imports. The plugin injects self.__WB_MANIFEST at build time
// with a versioned list of every static asset the Vite build emits.
// =============================================================================

import { precacheAndRoute, cleanupOutdatedCaches, createHandlerBoundToURL } from 'workbox-precaching';
import { registerRoute, NavigationRoute } from 'workbox-routing';
import { CacheFirst } from 'workbox-strategies';
import { ExpirationPlugin } from 'workbox-expiration';

// ---------------------------------------------------------------------------
// Precaching
// ---------------------------------------------------------------------------

// Workbox caches every asset listed in the injected manifest during SW install.
// On future visits the assets are served straight from cache — fast and offline.
// Each asset entry includes a revision hash, so stale files are replaced
// automatically when you deploy a new build.
precacheAndRoute(self.__WB_MANIFEST);

// Remove caches that belong to older SW versions to avoid wasting storage.
cleanupOutdatedCaches();

// ---------------------------------------------------------------------------
// Routing
// ---------------------------------------------------------------------------

// Single-page app navigation fallback — every navigation request (typing a URL,
// clicking a link) is answered with the cached index.html. React Router then
// handles the correct page client-side. This is what makes the app load
// instantly and work offline after the first visit.
const spaHandler = createHandlerBoundToURL('/index.html');
registerRoute(new NavigationRoute(spaHandler));

// Images rarely change, so serve them cache-first with a 30-day expiry.
// Workbox evicts the oldest entries once the cache exceeds 30 items.
registerRoute(
    ({ request }) => request.destination === 'image',
    new CacheFirst({
        cacheName: 'pnp-images',
        plugins: [
            new ExpirationPlugin({
                maxEntries: 30,
                maxAgeSeconds: 30 * 24 * 60 * 60,
            }),
        ],
    })
);

// ---------------------------------------------------------------------------
// App update lifecycle
// ---------------------------------------------------------------------------

// The PwaUpdateBanner sends { type: 'SKIP_WAITING' } when the user clicks
// "Refresh". This tells the new (waiting) SW to activate immediately instead
// of waiting for all tabs to close.
self.addEventListener('message', (event) => {
    if (event.data?.type === 'SKIP_WAITING') {
        self.skipWaiting();
    }
});

// ---------------------------------------------------------------------------
// Push notifications
// ---------------------------------------------------------------------------

self.addEventListener('push', (event) => {
    let payload = {};
    try {
        payload = event.data ? event.data.json() : {};
    } catch {
        payload = { title: 'Pub Night Picker', body: 'You have a new notification.' };
    }

    const title = payload.title || 'Pub Night Picker';
    const body = payload.body || 'You have a new notification.';
    const url = payload.url || '/';
    const tag = payload.tag || 'pubnightpicker';

    const notification = {
        body,
        tag,
        data: {
            url,
            eventType: payload.eventType || null,
            pollId: payload.pollId || null,
        },
    };

    const broadcast = async () => {
        const clientList = await self.clients.matchAll({ type: 'window', includeUncontrolled: true });
        for (const client of clientList) {
            client.postMessage({
                type: 'push-received',
                notification: payload,
            });
        }
    };

    event.waitUntil(Promise.all([self.registration.showNotification(title, notification), broadcast()]));
});

self.addEventListener('notificationclick', (event) => {
    event.notification.close();
    const targetUrl = event.notification?.data?.url || '/';

    event.waitUntil(
        self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then((clientList) => {
            for (const client of clientList) {
                const currentPath = new URL(client.url).pathname;
                const targetPath = new URL(targetUrl, self.location.origin).pathname;
                if (currentPath === targetPath && 'focus' in client) {
                    return client.focus();
                }
            }
            if (self.clients.openWindow) {
                return self.clients.openWindow(targetUrl);
            }
            return undefined;
        }),
    );
});

self.addEventListener('pushsubscriptionchange', (event) => {
    const broadcast = async (message) => {
        const clientList = await self.clients.matchAll({ type: 'window', includeUncontrolled: true });
        for (const client of clientList) {
            client.postMessage(message);
        }
    };

    const recoverSubscription = async () => {
        try {
            let subscription = event.newSubscription || null;

            if (!subscription) {
                const oldOptions = event.oldSubscription?.options || {};
                const subscribeOptions = {
                    userVisibleOnly: oldOptions.userVisibleOnly !== false,
                };

                if (oldOptions.applicationServerKey) {
                    subscribeOptions.applicationServerKey = oldOptions.applicationServerKey;
                }

                subscription = await self.registration.pushManager.subscribe(subscribeOptions);
            }

            await broadcast({
                type: 'push-subscription-changed',
                subscription: subscription?.toJSON?.() || null,
            });
        } catch (error) {
            console.error('Failed to recover push subscription', error);
            await broadcast({
                type: 'push-subscription-change-failed',
            });
        }
    };

    event.waitUntil(recoverSubscription());
});
