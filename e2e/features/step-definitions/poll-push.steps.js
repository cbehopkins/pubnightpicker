import { strict as assert } from "node:assert";
import { When, Then } from "@cucumber/cucumber";
import { collection, doc, getDocFromServer, getDocsFromServer, query, where } from "firebase/firestore";
import { waitForValue, withEmulatorAdmin } from "../support/emulator.js";
import { readBackendRecords, waitForBackendRecord } from "../support/backend-evidence.js";
import { londonDate, shiftDate } from "../support/calendar.js";

const BASE_URL = process.env.E2E_BASE_URL ?? "http://127.0.0.1:3000";
let pollDateOffset = 4;

function pollPanel(world, actor) {
    return world.actors.get(actor).getByRole("heading", { name: world.notificationPoll.date, exact: true }).locator("../..");
}

function eventPanel(world, actor) {
    const page = world.actors.get(actor);
    return page.getByRole("article").filter({
        has: page.getByRole("heading", { name: world.journeyVenues.get(world.notificationPoll.venue), exact: true }),
    }).filter({ hasText: world.notificationPoll.date });
}

async function venueId(world, name) {
    return withEmulatorAdmin(async (db) => {
        const venues = await getDocsFromServer(query(collection(db, "pubs"), where("name", "==", world.journeyVenues.get(name))));
        assert.equal(venues.size, 1);
        return venues.docs[0].id;
    });
}

async function pollSnapshot(world, db) {
    return getDocFromServer(doc(db, "polls", world.notificationPoll.id));
}

When("the poll push members save these preferences:", { timeout: 120_000 }, async function (table) {
    this.actorUids ??= new Map();
    this.actorPushEndpointIds ??= new Map();
    for (const { actor, enabled, opens, completes } of table.hashes()) {
        for (const flag of [enabled, opens, completes]) assert.ok(["true", "false"].includes(flag));
        const page = this.actors.get(actor);
        await page.getByRole("link", { name: "My Preferences", exact: true }).click();
        await page.getByRole("button", { name: "Enable Push", exact: true }).click();
        await page.getByText("Status: Enabled | Permission: Granted", { exact: true }).waitFor();
        for (const [label, value] of [
            ["A poll opens", opens], ["A poll completes", completes],
            ["A message is sent in global chat", "false"],
            ["A message is sent in an event chat I am attending", "false"],
        ]) {
            await page.getByRole("checkbox", { name: label, exact: true }).setChecked(value === "true");
        }
        await page.getByRole("button", { name: "Save", exact: true }).click();
        await page.waitForURL(`${BASE_URL}/`);
        if (enabled === "false") {
            await page.getByRole("link", { name: "My Preferences", exact: true }).click();
            await page.getByRole("button", { name: "Disable Push", exact: true }).click();
            await page.getByText("Status: Disabled | Permission: Granted", { exact: true }).waitFor();
        }
        await withEmulatorAdmin(async (db) => {
            const users = await getDocsFromServer(query(collection(db, "users"), where("email", "==", this.actorEmails.get(actor))));
            assert.equal(users.size, 1);
            const uid = users.docs[0].id;
            this.actorUids.set(actor, uid);
            const endpoints = await getDocsFromServer(collection(db, "users", uid, "push_endpoints"));
            assert.equal(endpoints.size, 1);
            this.actorPushEndpointIds.set(actor, endpoints.docs[0].id);
        });
        const uid = this.actorUids.get(actor);
        await waitForBackendRecord((record) =>
            record.msg === "notification profile projection applied" && record.contribution === "private_user" &&
            record.user_id === uid && record.web_push_enabled === (enabled === "true") &&
            record.poll_opens === (opens === "true") && record.poll_completes === (completes === "true"),
            `${actor}'s poll preferences projected`);
        await waitForBackendRecord((record) =>
            record.msg === "notification profile projection applied" && record.contribution === "push_endpoint" &&
            record.user_id === uid && record.endpoint_id === this.actorPushEndpointIds.get(actor) &&
            record.active === (enabled === "true"), `${actor}'s endpoint projected`);
    }
});

When("{word} creates the notification journey poll", async function (actor) {
    this.notificationPoll = { date: shiftDate(londonDate(), pollDateOffset++) };
    const page = this.actors.get(actor);
    await page.getByRole("link", { name: "Active Polls", exact: true }).click();
    await page.locator('input[type="date"]').fill(this.notificationPoll.date);
    await page.getByRole("button", { name: "Add Poll", exact: true }).click();
    await pollPanel(this, actor).waitFor();
    this.notificationPoll.id = await withEmulatorAdmin(async (db) => {
        const polls = await waitForValue(() => getDocsFromServer(query(collection(db, "polls"),
            where("date", "==", this.notificationPoll.date), where("createdByUid", "==", this.actorUids.get(actor)))),
            (result) => !result.empty, "UI-created notification poll");
        assert.equal(polls.size, 1);
        return polls.docs[0].id;
    });
});

When("{word} adds {string} to the notification journey poll", async function (actor, venue) {
    const page = this.actors.get(actor);
    const panel = pollPanel(this, actor);
    const picker = panel.getByRole("combobox").filter({
        has: page.getByRole("option", { name: "Select a venue to add here", exact: true }),
    });
    await picker.selectOption({ label: this.journeyVenues.get(venue) });
    await panel.getByRole("button", { name: "Add Venue To Poll", exact: true }).click();
    await panel.getByRole("row").filter({ hasText: this.journeyVenues.get(venue) }).waitFor();
});

When("{word} manually completes the notification journey poll at {string}", async function (actor, venue) {
    const page = this.actors.get(actor);
    await pollPanel(this, actor).getByRole("button", { name: this.journeyVenues.get(venue), exact: true }).click();
    await page.getByRole("heading", { name: "Complete Poll", exact: true }).waitFor();
    await withEmulatorAdmin((db) => waitForValue(async () => {
        const [request, ack] = await Promise.all([
            getDocFromServer(doc(db, "notification_req", this.notificationPoll.id)),
            getDocFromServer(doc(db, "notification_ack", this.notificationPoll.id)),
        ]);
        return request.data()?.complete !== undefined && request.data().complete === ack.data()?.complete;
    }, Boolean, "successful completion notification handshake"));
    const unexpectedDialogs = [];
    const rejectDialog = async (dialog) => {
        unexpectedDialogs.push(dialog.message());
        await dialog.dismiss();
    };
    page.on("dialog", rejectDialog);
    try {
        await waitForValue(async () => {
            unexpectedDialogs.length = 0;
            await page.getByRole("button", { name: "Confirm", exact: true }).click();
            for (const warning of unexpectedDialogs) {
                assert.match(warning, /^Notification tool has not acknowledged the completion handshake yet\./);
            }
            return unexpectedDialogs.length === 0;
        }, Boolean, "browser completion handshake ready without accepting a warning");
        await page.getByRole("heading", { name: "Complete Poll", exact: true }).waitFor({ state: "hidden" });
    } finally {
        page.off("dialog", rejectDialog);
    }
    this.notificationPoll.venue = venue;
    this.notificationPoll.venueId = await venueId(this, venue);
    await withEmulatorAdmin((db) => waitForValue(() => pollSnapshot(this, db),
        (snapshot) => snapshot.data()?.completed === true && snapshot.data()?.selected === this.notificationPoll.venueId,
        "manual poll completion persisted"));
});

async function openReschedule(world, actor) {
    const page = world.actors.get(actor);
    await page.getByRole("link", { name: "Current Events", exact: true }).click();
    await eventPanel(world, actor).getByRole("button", { name: "Reschedule Event", exact: true }).click();
    await page.getByRole("heading", { name: "Reschedule Event", exact: true }).waitFor();
    return page;
}

async function saveReschedule(world, page) {
    await page.getByRole("button", { name: "Save Changes", exact: true }).click();
    await page.getByRole("heading", { name: "Reschedule Event", exact: true }).waitFor({ state: "hidden" });
    const poll = world.notificationPoll;
    await withEmulatorAdmin((db) => waitForValue(() => pollSnapshot(world, db), (snapshot) => {
        const data = snapshot.data();
        return data?.selected === poll.venueId && (data.restaurant ?? "") === (poll.restaurantId ?? "") &&
            (data.restaurant_time ?? "") === (poll.restaurantTime ?? "") && data.completed === true;
    }, "UI reschedule persisted"));
}

When("{word} reschedules the notification journey event to {string}", async function (actor, venue) {
    const page = await openReschedule(this, actor);
    await page.getByRole("combobox").filter({
        has: page.getByRole("option", { name: "Select a main venue", exact: true }),
    }).selectOption({ label: this.journeyVenues.get(venue) });
    this.notificationPoll.venue = venue;
    this.notificationPoll.venueId = await venueId(this, venue);
    await saveReschedule(this, page);
});

When("{word} sets the notification journey meal to {string} at {string}", async function (actor, restaurant, time) {
    const page = await openReschedule(this, actor);
    await page.getByLabel("Restaurant", { exact: true }).selectOption({ label: this.journeyVenues.get(restaurant) });
    await page.getByLabel("Restaurant meetup time", { exact: true }).fill(time);
    this.notificationPoll.restaurant = restaurant;
    this.notificationPoll.restaurantId = await venueId(this, restaurant);
    this.notificationPoll.restaurantTime = time;
    await saveReschedule(this, page);
});

Then("Last Orders submits the journey poll's {string} push only to:", async function (eventType, table) {
    assert.ok(["poll_opened", "poll_completed", "poll_rescheduled"].includes(eventType));
    const poll = this.notificationPoll;
    const opened = eventType === "poll_opened";
    const key = opened ? poll.id : poll.restaurantId || poll.restaurantTime
        ? `${poll.venueId}:${poll.restaurantId ?? ""}:${poll.restaurantTime ?? ""}` : poll.venueId;
    await withEmulatorAdmin((db) => waitForValue(async () =>
        (await getDocFromServer(doc(db, opened ? "open_actions" : "comp_actions", poll.id))).data()?.push,
        (keys) => Array.isArray(keys) && keys.includes(key), `${eventType} push processing completed`));

    let body = opened ? "Voting has opened for this week's pub night. Tap to open the active polls page."
        : `${poll.date}: ${this.journeyVenues.get(poll.venue)}.`;
    if (!opened && poll.restaurant) body += ` Pre-pub meal at ${this.journeyVenues.get(poll.restaurant)}.`;
    if (!opened && poll.restaurantTime) body += ` Meet at ${poll.restaurantTime}.`;
    const title = opened ? "Pub Night voting opened" : eventType === "poll_rescheduled"
        ? `Pub Night rescheduled: ${this.journeyVenues.get(poll.venue)}` : `Pub Night @ ${this.journeyVenues.get(poll.venue)}`;
    const url = `${BASE_URL}/${opened ? "active_polls" : "current_events"}`;
    const tag = `${opened ? "poll-open" : "poll-complete"}:${poll.id}`;
    this.expectedPollPushes ??= [];
    for (const { recipient } of table.hashes()) {
        assert.ok(this.actorUids.has(recipient), `Unknown recipient ${recipient}`);
        this.expectedPollPushes.push({ endpoint: this.pushIdentities.get(recipient).endpoint, eventType, title, body, url, tag });
    }
    const records = await readBackendRecords();
    const sends = records.flatMap((record) => {
        if (record.msg !== "dummy push sent" || typeof record.message !== "string") return [];
        const payload = JSON.parse(record.message);
        if (payload.pollId !== poll.id) return [];
        assert.ok(Number.isFinite(Date.parse(payload.sentAt)), "Push must include a timestamp");
        return [{
            endpoint: record.endpoint, eventType: payload.eventType, title: payload.title,
            body: payload.body, url: payload.url, tag: payload.tag
        }];
    });
    const canonical = (items) => items.map((item) => JSON.stringify(item)).sort();
    const actual = canonical(sends);
    const expected = canonical(this.expectedPollPushes);
    assert.equal(actual.length, expected.length, `Unexpected cumulative push count for poll ${poll.id}`);
    expected.forEach((value, index) => assert.equal(actual[index], value, `Unexpected poll push at index ${index}`));
    for (const identity of this.pushIdentities.values()) {
        assert.equal(identity.deliveryRequests.length, 0, "No external push delivery request is allowed");
    }
});

Then("{word} sees the notification journey event at {string} without voting", async function (actor, venue) {
    assert.equal(this.notificationPoll.venue, venue);
    await this.actors.get(actor).getByRole("link", { name: "Current Events", exact: true }).click();
    await eventPanel(this, actor).waitFor();
    await withEmulatorAdmin(async (db) => {
        const votes = await getDocFromServer(doc(db, "votes", this.notificationPoll.id));
        assert.ok(!JSON.stringify(votes.data() ?? {}).includes(this.actorUids.get(actor)), `${actor} must not have voted`);
    });
});

Then("the poll push members disable push notifications", { timeout: 120_000 }, async function () {
    for (const [actor, uid] of this.actorUids) {
        const page = this.actors.get(actor);
        await page.getByRole("link", { name: "My Preferences", exact: true }).click();
        const disable = page.getByRole("button", { name: "Disable Push", exact: true });
        await page.getByRole("heading", { name: "Web Push Notifications", exact: true }).waitFor();
        await page.getByText(/^Status: (Enabled|Disabled) \| Permission: Granted$/).waitFor();
        if (await disable.isEnabled()) await disable.click();
        await page.getByText("Status: Disabled | Permission: Granted", { exact: true }).waitFor();
        await waitForBackendRecord((record) =>
            record.msg === "notification profile projection applied" && record.contribution === "private_user" &&
            record.user_id === uid && record.web_push_enabled === false, `${actor}'s push opt-out projected`);
        await waitForBackendRecord((record) =>
            record.msg === "notification profile projection applied" && record.contribution === "push_endpoint" &&
            record.user_id === uid && record.endpoint_id === this.actorPushEndpointIds.get(actor) && record.active === false,
            `${actor}'s endpoint deactivation projected`);
    }
});
