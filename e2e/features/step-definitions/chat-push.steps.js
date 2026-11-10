import { strict as assert } from "node:assert";
import { randomUUID } from "node:crypto";
import { Given, When, Then } from "@cucumber/cucumber";
import { collection, doc, getDocFromServer, getDocsFromServer, query, where } from "firebase/firestore";
import { waitForValue, withEmulatorAdmin } from "../support/emulator.js";
import { readBackendRecords, waitForBackendRecord } from "../support/backend-evidence.js";

const BASE_URL = process.env.E2E_BASE_URL ?? "http://127.0.0.1:3000";
const PREFERENCE_FIELDS = new Map([["global", "global_chat"], ["event", "event_chat"]]);

function categoryTable(category, enabled) {
    return [
        { category: "A poll opens", enabled: "false" },
        { category: "A poll completes", enabled: "false" },
        { category: "A message is sent in global chat", enabled: category === "globalChat" ? enabled : "false" },
        { category: "A message is sent in an event chat I am attending", enabled: category === "eventChat" ? enabled : "false" },
    ];
}

Given("notification suppression is off in the fresh emulator", async function () {
    await withEmulatorAdmin(async (db) => {
        const config = await getDocFromServer(doc(db, "config", "diagnostics"));
        assert.ok(!config.exists() || config.data().SilenceNotifications !== true,
            "Fresh test data must not silence notification submission");
    });
});

When("{word} enables push and saves {word} chat notifications as {string}", async function (name, scope, enabled) {
    assert.ok(PREFERENCE_FIELDS.has(scope));
    assert.ok(["true", "false"].includes(enabled));
    const page = this.actors.get(name);
    await page.getByRole("link", { name: "My Preferences", exact: true }).click();
    await page.waitForURL(`${BASE_URL}/preferences`);
    await page.getByRole("button", { name: "Enable Push", exact: true }).click();
    await page.getByText("Status: Enabled | Permission: Granted", { exact: true }).waitFor();
    const category = scope === "global" ? "globalChat" : "eventChat";
    const rows = categoryTable(category, enabled);
    for (const { category: label, enabled: value } of rows) {
        await page.getByRole("checkbox", { name: label, exact: true }).setChecked(value === "true");
    }
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await page.waitForURL(`${BASE_URL}/`);
    await withEmulatorAdmin(async (db) => {
        const snapshot = await waitForValue(async () => {
            const users = await getDocsFromServer(query(collection(db, "users"), where("email", "==", this.actorEmails.get(name))));
            return users.empty ? null : users.docs[0];
        }, (user) => user !== null, `${name} profile after saving chat preferences`);
        this.actorUids ??= new Map();
        this.actorUids.set(name, snapshot.id);
        const endpoints = await getDocsFromServer(collection(db, "users", snapshot.id, "push_endpoints"));
        assert.equal(endpoints.size, 1);
        this.actorPushEndpointIds ??= new Map();
        this.actorPushEndpointIds.set(name, endpoints.docs[0].id);
    });
});

Then("Last Orders observes these push profiles:", async function (table) {
    for (const { actor, global, event } of table.hashes()) {
        assert.ok(this.actorUids?.has(actor), `No registered user for ${actor}`);
        assert.ok(this.actorPushEndpointIds?.has(actor), `No push endpoint for ${actor}`);
        const userId = this.actorUids.get(actor);
        const endpointId = this.actorPushEndpointIds.get(actor);
        const profile = await waitForBackendRecord((record) =>
            record.msg === "notification profile projection applied" &&
            record.contribution === "private_user" && record.user_id === userId &&
            record.web_push_enabled === true && record.global_chat === (global === "true") &&
            record.event_chat === (event === "true"), `${actor} chat preferences projected`);
        assert.equal(profile.user_id, userId);
        const endpoint = await waitForBackendRecord((record) =>
            record.msg === "notification profile projection applied" &&
            record.contribution === "push_endpoint" && record.user_id === userId &&
            record.endpoint_id === endpointId && record.active === true,
            `${actor} active push endpoint projected`);
        assert.equal(endpoint.endpoint_id, endpointId);
    }
});

async function messageSnapshot(world, actor, scopeType, scopeId) {
    const text = world.chatMessages?.[scopeType === "global" ? "global" : "event"];
    assert.ok(text, `No ${scopeType} chat message was sent`);
    return withEmulatorAdmin((db) => waitForValue(async () => {
        const messages = await getDocsFromServer(query(collection(db, "messages"), where("text", "==", text)));
        return messages.empty ? null : messages.docs[0];
    }, (snapshot) => snapshot !== null, `Firestore message for ${scopeType} chat`)).then((snapshot) => {
        assert.equal(snapshot.data().uid, world.actorUids.get(actor));
        assert.equal(snapshot.data().scopeType, scopeType);
        assert.equal(snapshot.data().scopeId, scopeId);
        return { id: snapshot.id, text };
    });
}

async function assertChatPush(world, actor, scopeType, scopeId, recipientNames) {
    const { id: messageId, text } = await messageSnapshot(world, actor, scopeType, scopeId);
    const eventType = scopeType === "global" ? "chat_message_sent_global" : "chat_message_sent_event";
    const expectedUrl = scopeType === "global" ? `${BASE_URL}/chat` : `${BASE_URL}/chat/event/${scopeId}`;
    await waitForBackendRecord((record) =>
        record.msg === "chat push processing completed" && record.message_id === messageId &&
        record.scope_type === scopeType && record.scope_id === scopeId,
        `${scopeType} push processing for message ${messageId}`);

    const records = await readBackendRecords();
    const sends = records.flatMap((record) => {
        if (record.msg !== "dummy push sent" || typeof record.message !== "string") return [];
        const payload = JSON.parse(record.message);
        if (payload.messageId !== messageId) return [];
        return [{ endpoint: record.endpoint, payload }];
    });
    const expectedEndpoints = recipientNames.map((name) => thisEndpoint(world, name)).sort();
    const actualEndpoints = sends.map(({ endpoint }) => endpoint).sort();
    assert.equal(actualEndpoints.length, expectedEndpoints.length, `Unexpected ${scopeType} push count for ${messageId}`);
    expectedEndpoints.forEach((endpoint, index) => assert.equal(actualEndpoints[index], endpoint, `Unexpected ${scopeType} recipient at index ${index}`));
    for (const { endpoint, payload } of sends) {
        assert.ok(expectedEndpoints.includes(endpoint));
        assert.equal(payload.messageId, messageId);
        assert.equal(payload.eventType, eventType);
        assert.equal(payload.body, text);
        assert.equal(payload.url, expectedUrl);
        if (scopeType === "event") assert.equal(payload.pollId, scopeId);
        else assert.equal(payload.pollId, null);
    }
}

function thisEndpoint(world, name) {
    const identity = world.pushIdentities.get(name);
    assert.ok(identity, `No synthetic subscription for ${name}`);
    assert.equal(identity.deliveryRequests.length, 0, "No external push delivery request is allowed");
    return identity.endpoint;
}

Then("Last Orders processes Asher's global chat push for:", async function (table) {
    await assertChatPush(this, "Asher", "global", "main", table.hashes().map(({ recipient }) => recipient));
});

When("{word} creates a poll through the UI for event chat", async function (name) {
    const page = this.actors.get(name);
    this.eventChatPollDate = await page.evaluate(() => {
        const date = new Date();
        date.setDate(date.getDate() + 2);
        return [date.getFullYear(), String(date.getMonth() + 1).padStart(2, "0"), String(date.getDate()).padStart(2, "0")].join("-");
    });
    await page.getByRole("link", { name: "Active Polls", exact: true }).click();
    await page.locator('input[type="date"]').fill(this.eventChatPollDate);
    await page.getByRole("button", { name: "Add Poll", exact: true }).click();
    await page.getByRole("heading", { name: this.eventChatPollDate, exact: true }).waitFor();
    const uid = this.actorUids.get(name);
    this.eventChatPollId = await withEmulatorAdmin(async (db) => {
        const polls = await waitForValue(async () => {
            const result = await getDocsFromServer(query(collection(db, "polls"), where("date", "==", this.eventChatPollDate), where("createdByUid", "==", uid)));
            return result.empty ? null : result;
        }, (result) => result !== null, `UI-created event chat poll for ${name}`);
        assert.equal(polls.size, 1);
        return polls.docs[0].id;
    });
});

When("{word} joins the event chat conversation", async function (name) {
    const page = this.actors.get(name);
    await page.getByRole("link", { name: "Active Polls", exact: true }).click();
    await page.getByRole("heading", { name: this.eventChatPollDate, exact: true }).waitFor();
    await page.getByRole("button", { name: "Event Chat", exact: true }).click();
    await page.getByRole("heading", { name: "Event Chat", exact: true }).waitFor();
    const text = `event-join-${name}-${randomUUID()}`;
    await page.getByLabel("Chat message", { exact: true }).fill(text);
    await page.getByRole("button", { name: "Send", exact: true }).click();
    await page.getByText(text, { exact: true }).waitFor();
    await withEmulatorAdmin(async (db) => {
        const messages = await waitForValue(async () => {
            const result = await getDocsFromServer(query(collection(db, "messages"), where("text", "==", text)));
            return result.empty ? null : result;
        }, (result) => result !== null, `${name} event chat participation`);
        assert.equal(messages.size, 1);
        assert.equal(messages.docs[0].data().uid, this.actorUids.get(name));
        assert.equal(messages.docs[0].data().scopeType, "event");
        assert.equal(messages.docs[0].data().scopeId, this.eventChatPollId);
        const messageId = messages.docs[0].id;
        await waitForBackendRecord((record) => record.msg === "chat push processing completed" && record.message_id === messageId, `${name}'s event chat message processed`);
    });
});

When("{word} mutes notifications for the journey event", async function (name) {
    const page = this.actors.get(name);
    await page.getByRole("button", { name: "Mute event chat notifications", exact: true }).click();
    await page.getByText("Event chat notifications are muted for this event.", { exact: true }).waitFor();
});

Then("Last Orders observes Hal's event-chat mute", async function () {
    const userId = this.actorUids.get("Hal");
    const pollId = this.eventChatPollId;
    await waitForBackendRecord((record) =>
        record.msg === "notification profile projection applied" &&
        record.contribution === "private_user" && record.user_id === userId &&
        Array.isArray(record.event_chat_muted_poll_ids) && record.event_chat_muted_poll_ids.includes(pollId),
        `Hal's mute for event ${pollId}`);
});

When("{word} sends the final event chat message", async function (name) {
    const page = this.actors.get(name);
    const text = `event-final-${randomUUID()}`;
    this.chatMessages ??= {};
    this.chatMessages.event = text;
    await page.getByRole("link", { name: "Active Polls", exact: true }).click();
    await page.getByRole("heading", { name: this.eventChatPollDate, exact: true }).waitFor();
    await page.getByRole("button", { name: "Event Chat", exact: true }).click();
    await page.getByRole("heading", { name: "Event Chat", exact: true }).waitFor();
    await page.getByLabel("Chat message", { exact: true }).fill(text);
    await page.getByRole("button", { name: "Send", exact: true }).click();
    await page.getByText(text, { exact: true }).waitFor();
});

Then("Last Orders processes Eli's event chat push for:", async function (table) {
    await assertChatPush(this, "Eli", "event", this.eventChatPollId, table.hashes().map(({ recipient }) => recipient));
});
