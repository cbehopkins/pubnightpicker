import { strict as assert } from "node:assert";
import { randomUUID } from "node:crypto";
import { When, Then } from "@cucumber/cucumber";
import { collection, doc, getDocFromServer, getDocsFromServer, query, where } from "firebase/firestore";
import { waitForValue, withEmulatorAdmin } from "../support/emulator.js";
import { readBackendRecords, waitForBackendRecord } from "../support/backend-evidence.js";

const BASE_URL = process.env.E2E_BASE_URL ?? "http://127.0.0.1:3000";
const MAILING_LIST = "ampubnight@googlegroups.com";

async function saveEmailPreferences(world, actor, opens, completes, address) {
    const page = world.actors.get(actor);
    const uid = world.actorUids.get(actor);
    const previous = await withEmulatorAdmin(async (db) => (await getDocFromServer(doc(db, "users", uid))).data());
    await page.getByRole("link", { name: "My Preferences", exact: true }).click();
    await page.waitForFunction(({ email, openEmail, completeEmail }) =>
        document.querySelector("#email")?.value === email &&
        document.querySelector("#open_poll_email")?.checked === openEmail &&
        document.querySelector("#emailme")?.checked === completeEmail, {
        email: previous.notificationEmail || previous.email,
        openEmail: previous.openPollEmailEnabled === true,
        completeEmail: previous.notificationEmailEnabled === true,
    });
    await page.getByLabel("Email Address", { exact: true }).fill(address);
    await page.getByRole("checkbox", { name: "Email me when a poll opens", exact: true }).setChecked(opens);
    await page.getByRole("checkbox", { name: "Email Me", exact: true }).setChecked(completes);
    const unchanged = previous.notificationEmail === address && previous.openPollEmailEnabled === opens &&
        previous.notificationEmailEnabled === completes;
    const baseline = unchanged ? 0 : (await readBackendRecords()).length;
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await page.waitForURL(`${BASE_URL}/`);
    await withEmulatorAdmin((db) => waitForValue(async () =>
        (await getDocFromServer(doc(db, "users", uid))).data(), (user) =>
        user?.notificationEmail === address && user.openPollEmailEnabled === opens && user.notificationEmailEnabled === completes,
    `${actor}'s saved email preferences`));
    await waitForValue(readBackendRecords, (records) => records.slice(baseline).some((record) =>
        record.msg === "notification profile projection applied" && record.contribution === "private_user" &&
        record.user_id === uid && record.open_poll_email_enabled === opens && record.notification_email_enabled === completes),
    `${actor}'s updated email preferences projected`);
}

When("the poll email members save these preferences:", { timeout: 120_000 }, async function (table) {
    await waitForBackendRecord((record) => record.msg === "email plugin configured" && record.client === "dummy",
        "dummy email client configured");
    this.actorUids ??= new Map();
    this.actorNotificationEmails = new Map();
    for (const { actor, opens, completes } of table.hashes()) {
        for (const flag of [opens, completes]) assert.ok(["true", "false"].includes(flag));
        const uid = await withEmulatorAdmin(async (db) => {
            const users = await getDocsFromServer(query(collection(db, "users"), where("email", "==", this.actorEmails.get(actor))));
            assert.equal(users.size, 1);
            return users.docs[0].id;
        });
        this.actorUids.set(actor, uid);
        await waitForBackendRecord((record) => record.msg === "notification profile projection applied" &&
            record.contribution === "private_user" && record.user_id === uid, `${actor}'s initial profile projected`);
        const address = `${actor.toLowerCase()}-${randomUUID()}@notifications.test.local`;
        assert.notEqual(address, this.actorEmails.get(actor));
        this.actorNotificationEmails.set(actor, address);
        await saveEmailPreferences(this, actor, opens === "true", completes === "true", address);
    }
});

Then("Last Orders submits the journey poll's {string} personal email only to:", async function (eventType, table) {
    assert.ok(["poll_opened", "poll_completed", "poll_rescheduled"].includes(eventType));
    const poll = this.notificationPoll;
    const opened = eventType === "poll_opened";
    const rescheduled = eventType === "poll_rescheduled";
    const key = opened ? poll.id : poll.restaurantId || poll.restaurantTime
        ? `${poll.venueId}:${poll.restaurantId ?? ""}:${poll.restaurantTime ?? ""}` : poll.venueId;
    const actions = opened ? ["email"] : ["email", "pemail"];
    await withEmulatorAdmin((db) => waitForValue(async () =>
        (await getDocFromServer(doc(db, opened ? "open_actions" : "comp_actions", poll.id))).data(),
    (record) => actions.every((action) => record?.[action]?.includes(key)), `${eventType} email submissions accepted`));

    const variables = opened ? {} : { venue_name: this.journeyVenues.get(poll.venue), event_date: poll.date };
    if (!opened && poll.restaurant) {
        variables.restaurant_name = this.journeyVenues.get(poll.restaurant);
        variables.restaurant_time = poll.restaurantTime;
    }
    const subject = opened ? "Pub Night voting opened" : rescheduled
        ? "Pub Night @ RESCHEDULED::{{venue_name}}" : "Pub Night @ {{venue_name}}";
    let text = opened ? `Voting has opened for this week's pub night.\nPlease visit ${BASE_URL}/active_polls\nto participate in the voting.\n`
        : "Every week we have a pub night to which you are cordially invited.\n" +
          "The venue changes each week, though we do tend to frequent a few favourites.\n" +
          "Suggestions for venues are always welcome.\n" +
          "The earliest attendees get there between 6:30 & 7:30pm and we continue through to closing time.\n";
    if (!opened && poll.restaurant) {
        text += `\nBefore the pub we are meeting at ${variables.restaurant_name}. We will be meeting there at ${poll.restaurantTime}.\n\n`;
    }
    if (!opened) text += `This week on ${poll.date} we will be visiting ${variables.venue_name}`;
    if (rescheduled) text = `This week's event has been rescheduled\n\n${text}`;

    this.expectedPollEmails ??= [];
    const personalToken = opened ? `poll-opened:${poll.id}` : `poll-completed:${poll.id}:pemail:${key}`;
    for (const { recipient } of table.hashes()) {
        assert.ok(this.actorNotificationEmails.has(recipient), `Unknown email recipient ${recipient}`);
        this.expectedPollEmails.push({
            token: personalToken, recipient: this.actorNotificationEmails.get(recipient), subject, variables,
            text: opened ? text : `${text}\n\nUnsubscribe at ${BASE_URL}/preferences/${this.actorUids.get(recipient)}`,
        });
    }
    if (!opened) {
        this.expectedPollEmails.push({ token: `poll-completed:${poll.id}:email:${key}`, recipient: MAILING_LIST,
            subject, variables, text });
    }

    const records = await readBackendRecords();
    const requests = new Map();
    for (const record of records) {
        if (record.msg !== "dummy email submission prepared") continue;
        if (!(record.idempotency_token === `poll-opened:${poll.id}` ||
            record.idempotency_token.startsWith(`poll-opened:${poll.id}:`) ||
            record.idempotency_token.startsWith(`poll-completed:${poll.id}:`))) continue;
        assert.ok(record.message_id, "Prepared dummy submission must include its message ID");
        requests.set(record.message_id, record);
    }
    const actual = records.flatMap((record) => {
        if (!["dummy email sent", "dummy email suppressed (dry-run)"].includes(record.msg)) return [];
        const request = requests.get(record.message_id);
        if (!request) return [];
        assert.equal(record.msg, "dummy email sent", "Poll email must not be suppressed");
        assert.equal(record.dry_run, false);
        const token = request.idempotency_token.replace(/:actor:[^:]+$|:others$/, "");
        return [{ token, recipient: record.recipient, subject: request.subject,
            variables: request.variables ?? {}, text: record.message }];
    });
    const canonical = (items) => items.map(({ token, recipient, subject: title, variables: values, text: body }) =>
        JSON.stringify([token, recipient, title, Object.entries(values).sort(([left], [right]) => left.localeCompare(right)), body])).sort();
    const actualRows = canonical(actual);
    const expectedRows = canonical(this.expectedPollEmails);
    assert.equal(actualRows.length, expectedRows.length, `Unexpected cumulative email count for poll ${poll.id}`);
    expectedRows.forEach((value, index) => assert.equal(actualRows[index], value, `Unexpected poll email at index ${index}`));
});

Then("the poll email members disable email notifications", { timeout: 120_000 }, async function () {
    for (const [actor, address] of this.actorNotificationEmails) {
        await saveEmailPreferences(this, actor, false, false, address);
    }
});