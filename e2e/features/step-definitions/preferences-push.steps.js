import { strict as assert } from "node:assert";
import { Given, When, Then } from "@cucumber/cucumber";
import { collection, doc, getDocFromServer, getDocsFromServer, query, where } from "firebase/firestore";
import { waitForValue, withEmulatorAdmin } from "../support/emulator.js";

const BASE_URL = process.env.E2E_BASE_URL ?? "http://127.0.0.1:3000";
const CATEGORY_FIELDS = new Map([
    ["A poll opens", "pollOpens"], ["A poll completes", "pollCompletes"],
    ["A message is sent in global chat", "globalChat"],
    ["A message is sent in an event chat I am attending", "eventChat"],
]);

async function actorPushState(world, name, db) {
    world.actorUids ??= new Map();
    if (!world.actorUids.has(name)) {
        const users = await getDocsFromServer(query(collection(db, "users"), where("email", "==", world.actorEmails.get(name))));
        if (users.empty) return null;
        assert.equal(users.size, 1, "A unique registered email must identify the scenario's user");
        world.actorUids.set(name, users.docs[0].id);
    }
    const uid = world.actorUids.get(name);
    const [user, endpoints] = await Promise.all([
        getDocFromServer(doc(db, "users", uid)), getDocsFromServer(collection(db, "users", uid, "push_endpoints")),
    ]);
    return { user: user.data(), endpoints: endpoints.docs.map((snapshot) => ({ id: snapshot.id, ...snapshot.data() })) };
}

async function waitForPushState(world, name, matches, description) {
    return withEmulatorAdmin((db) => waitForValue(() => actorPushState(world, name, db), (value) => value !== null && matches(value), `${name}: ${description}`));
}

async function pushAudit(world, name) {
    assert.equal(world.pushIdentities.get(name).deliveryRequests.length, 0, "No request may contact the synthetic push delivery endpoint");
    return world.actors.get(name).evaluate(() => window.__journeyPush.snapshot());
}

Given("{word}'s browser will grant push permission", function (name) {
    assert.ok(!this.actors.has(name), "Permission response must be configured before opening the browser context");
    this.pushPermissionResponses ??= new Map();
    this.pushPermissionResponses.set(name, "granted");
});

Given("{word}'s browser will deny push permission", function (name) {
    assert.ok(!this.actors.has(name));
    this.pushPermissionResponses ??= new Map();
    this.pushPermissionResponses.set(name, "denied");
});

When("{word} visits My Preferences", async function (name) {
    const page = this.actors.get(name);
    await page.getByRole("link", { name: "My Preferences", exact: true }).click();
    await page.waitForURL(`${BASE_URL}/preferences`);
    await page.getByRole("heading", { name: "Web Push Notifications", exact: true }).waitFor();
    await page.getByRole("button", { name: "Enable Push", exact: true }).waitFor();
});

Then("{word} has not requested permission or subscribed to push", async function (name) {
    const audit = await pushAudit(this, name);
    assert.equal(audit.requestCalls, 0);
    assert.equal(audit.subscribeCalls, 0);
    assert.equal(audit.unsolicitedRequests, 0);
    assert.equal(audit.permission, "default");
});

Then("{word} starts with push disabled and no endpoint", async function (name) {
    await this.actors.get(name).getByText("Status: Disabled | Permission: Not requested", { exact: true }).waitFor();
    await waitForPushState(this, name, ({ user, endpoints }) => user.webPushEnabled !== true && endpoints.length === 0, "initially disabled without endpoints");
});

When("{word} clicks Enable Push", async function (name) {
    await this.actors.get(name).getByRole("button", { name: "Enable Push", exact: true }).click();
});

Then("{word} has an active synthetic push endpoint and enabled preference", async function (name) {
    const page = this.actors.get(name);
    await page.getByText("Status: Enabled | Permission: Granted", { exact: true }).waitFor();
    const identity = this.pushIdentities.get(name);
    const state = await waitForPushState(this, name, ({ user, endpoints }) => user.webPushEnabled === true && endpoints.length === 1 && endpoints[0].active === true, "active endpoint and opt-in");
    const endpoint = state.endpoints[0];
    assert.equal(endpoint.endpoint, identity.endpoint);
    assert.equal(endpoint.p256dh, identity.keys.p256dh);
    assert.equal(endpoint.auth, identity.keys.auth);
    assert.ok(endpoint.createdAt?.toMillis() > 0);
    assert.ok(endpoint.lastSeenAt?.toMillis() > 0);
    const audit = await pushAudit(this, name);
    assert.ok(audit.registrations.includes("/dev-sw.js"), "App must register the real local service worker");
    assert.equal(await page.evaluate(async () => (await navigator.serviceWorker.getRegistrations()).length > 0), true);
});

When("{word} saves these push notification categories:", async function (name, table) {
    const page = this.actors.get(name);
    this.pushCategoriesByActor ??= new Map();
    const categories = table.hashes().map(({ category, enabled }) => {
        assert.ok(CATEGORY_FIELDS.has(category));
        assert.ok(["true", "false"].includes(enabled));
        return { category, enabled: enabled === "true" };
    });
    this.pushCategoriesByActor.set(name, categories);
    for (const { category, enabled } of categories) {
        await page.getByRole("checkbox", { name: category, exact: true }).setChecked(enabled);
    }
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await page.waitForURL(`${BASE_URL}/`);
    await waitForPushState(this, name, ({ user }) => categories.every(({ category, enabled }) => user.pushPreferences?.[CATEGORY_FIELDS.get(category)] === enabled), "saved push category preferences");
});

When("{word} reloads My Preferences", async function (name) {
    const page = this.actors.get(name);
    await page.goto(`${BASE_URL}/preferences`, { waitUntil: "domcontentloaded" });
    await page.getByRole("heading", { name: "Web Push Notifications", exact: true }).waitFor();
    await page.getByRole("button", { name: "Enable Push", exact: true }).waitFor();
});

Then("{word} sees the saved push notification categories", async function (name) {
    const page = this.actors.get(name);
    await page.getByText("Status: Enabled | Permission: Granted", { exact: true }).waitFor();
    for (const { category, enabled } of this.pushCategoriesByActor.get(name) ?? []) {
        const checkbox = page.getByRole("checkbox", { name: category, exact: true });
        await checkbox.waitFor();
        await checkbox.and(page.locator(enabled ? ":checked" : ":not(:checked)")).waitFor();
        assert.equal(await checkbox.isChecked(), enabled, category);
    }
});

Then("{word} has made exactly one voluntary push subscription", async function (name) {
    const audit = await pushAudit(this, name);
    assert.equal(audit.enableClicks, 1);
    assert.equal(audit.requestCalls, 1);
    assert.equal(audit.subscribeCalls, 1);
    assert.equal(audit.unsolicitedRequests, 0);
    assert.equal(audit.subscribed, true);
});

Then("{word} sees push permission denied without a subscription", async function (name) {
    const page = this.actors.get(name);
    await page.getByText("Notification permission denied", { exact: true }).waitFor();
    await page.getByText("Status: Disabled | Permission: Denied", { exact: true }).waitFor();
    const audit = await pushAudit(this, name);
    assert.equal(audit.requestCalls, 1);
    assert.equal(audit.subscribeCalls, 0);
    assert.equal(audit.unsolicitedRequests, 0);
    await waitForPushState(this, name, ({ user, endpoints }) => user.webPushEnabled !== true && endpoints.length === 0, "permission denied without endpoint creation");
});

Then("{word} still has no push endpoint or enabled preference", async function (name) {
    await this.actors.get(name).getByText("Status: Disabled | Permission: Denied", { exact: true }).waitFor();
    await waitForPushState(this, name, ({ user, endpoints }) => user.webPushEnabled !== true && endpoints.length === 0, "denied preference after revisit");
    const audit = await pushAudit(this, name);
    assert.equal(audit.requestCalls, 1);
    assert.equal(audit.subscribeCalls, 0);
});

When("{word} clicks Disable Push", async function (name) {
    await this.actors.get(name).getByRole("button", { name: "Disable Push", exact: true }).click();
});

Then("{word} has an inactive push endpoint and disabled preference", async function (name) {
    await this.actors.get(name).getByText("Status: Disabled | Permission: Granted", { exact: true }).waitFor();
    const state = await waitForPushState(this, name, ({ user, endpoints }) => user.webPushEnabled === false && endpoints.length === 1 && endpoints[0].active === false, "disabled preference and inactive endpoint");
    assert.equal(state.endpoints[0].endpoint, this.pushIdentities.get(name).endpoint);
    assert.ok(state.endpoints[0].disabledAt?.toMillis() > 0);
    const audit = await pushAudit(this, name);
    assert.equal(audit.unsubscribeCalls, 1);
    assert.equal(audit.subscribed, false);
    assert.equal(audit.requestCalls, 1);
    assert.equal(audit.subscribeCalls, 1);
    assert.equal(audit.unsolicitedRequests, 0);
});
