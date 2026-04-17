self.addEventListener("push", (event) => {
    let payload = {};
    try {
        payload = event.data ? event.data.json() : {};
    } catch {
        payload = { title: "Pub Night Picker", body: "You have a new notification." };
    }

    const title = payload.title || "Pub Night Picker";
    const body = payload.body || "You have a new notification.";
    const url = payload.url || "/";
    const tag = payload.tag || "pubnightpicker";

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
        const clientList = await self.clients.matchAll({ type: "window", includeUncontrolled: true });
        for (const client of clientList) {
            client.postMessage({
                type: "push-received",
                notification: payload,
            });
        }
    };

    event.waitUntil(Promise.all([self.registration.showNotification(title, notification), broadcast()]));
});

self.addEventListener("notificationclick", (event) => {
    event.notification.close();
    const targetUrl = event.notification?.data?.url || "/";

    event.waitUntil(
        self.clients.matchAll({ type: "window", includeUncontrolled: true }).then((clientList) => {
            for (const client of clientList) {
                const currentPath = new URL(client.url).pathname;
                const targetPath = new URL(targetUrl, self.location.origin).pathname;
                if (currentPath === targetPath && "focus" in client) {
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

self.addEventListener("pushsubscriptionchange", (event) => {
    const broadcast = async (message) => {
        const clientList = await self.clients.matchAll({ type: "window", includeUncontrolled: true });
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
                type: "push-subscription-changed",
                subscription: subscription?.toJSON?.() || null,
            });
        } catch (error) {
            console.error("Failed to recover push subscription", error);
            await broadcast({
                type: "push-subscription-change-failed",
            });
        }
    };

    event.waitUntil(recoverSubscription());
});
