import { strict as assert } from "node:assert";
import { randomUUID } from "node:crypto";
import { Given, When, Then } from "@cucumber/cucumber";
import { initializeApp, deleteApp } from "firebase/app";
import { getAuth, connectAuthEmulator, signInWithEmailAndPassword } from "firebase/auth";
import {
    collection,
    connectFirestoreEmulator,
    doc,
    getDocFromServer,
    getDocsFromServer,
    getFirestore,
    query,
    terminate,
    where,
    writeBatch,
} from "firebase/firestore";
import { nextAvailablePollDate, waitForValue, withEmulatorAdmin } from "../support/emulator.js";

const BASE_URL = process.env.E2E_BASE_URL ?? "http://127.0.0.1:3000";

function journeyPoll(world, actor) {
    return world.actors.get(actor)
        .getByRole("heading", { name: world.pollDate, exact: true })
        .locator("../..");
}

function venueRow(world, actor, venue) {
    return journeyPoll(world, actor).getByRole("row")
        .filter({ hasText: world.journeyVenues.get(venue) });
}

Given("the following journey venues are available:", async function (table) {
    assert.equal(process.env.GOOGLE_CLOUD_PROJECT, "demo-pubnightpicker-journey");
    assert.equal(process.env.FIRESTORE_EMULATOR_HOST, "127.0.0.1:8080");
    assert.equal(process.env.FIREBASE_AUTH_EMULATOR_HOST, "127.0.0.1:9099");
    const fixtureId = randomUUID();
    const app = initializeApp({
        apiKey: "journey-emulator-key",
        projectId: "demo-pubnightpicker-journey",
    }, `poll-venues-${fixtureId}`);
    const auth = getAuth(app);
    connectAuthEmulator(auth, "http://127.0.0.1:9099", { disableWarnings: true });
    const db = getFirestore(app);
    connectFirestoreEmulator(db, "127.0.0.1", 8080);
    this.journeyVenues = new Map();
    this.journeyVenueTypes = new Map();
    try {
        await signInWithEmailAndPassword(auth, process.env.E2E_ADMIN_EMAIL, process.env.E2E_ADMIN_PASSWORD);
        const batch = writeBatch(db);
        for (const { name, type } of table.hashes()) {
            assert.ok(["pub", "restaurant"].includes(type));
            const displayName = `${name} ${fixtureId.slice(0, 8)}`;
            this.journeyVenues.set(name, displayName);
            this.journeyVenueTypes.set(name, type);
            batch.set(doc(db, "pubs", randomUUID()), {
                name: displayName,
                venueType: type,
                food: true,
                banned: false,
            });
        }
        await batch.commit();
    } finally {
        await terminate(db);
        await deleteApp(app);
    }
});

When("the admin grants {word} the following permissions:", async function (name, table) {
    const admin = this.actors.get("admin");
    await admin.getByRole("link", { name: "Admin", exact: true }).click();
    await admin.getByRole("heading", { name: "Manage Users", exact: true, level: 1 }).waitFor();
    await admin.getByText(this.actorEmails.get(name), { exact: true }).waitFor();
    await admin.getByRole("row").filter({ hasText: this.actorEmails.get(name) })
        .getByRole("link", { name, exact: true }).click();
    await admin.getByRole("heading", { name: "Manage User", exact: true }).waitFor();
    await admin.getByText(this.actorEmails.get(name), { exact: true }).waitFor();
    for (const { permission } of table.hashes()) {
        const checkbox = admin.getByRole("checkbox", { name: permission, exact: true });
        await checkbox.waitFor();
        assert.equal(await checkbox.isChecked(), false, `${name} must start without ${permission}`);
        await checkbox.click();
        await checkbox.and(admin.locator(":checked")).waitFor();
    }
    assert.equal(await admin.getByRole("checkbox", { name: "Admin", exact: true }).isChecked(), false);
    assert.equal(await admin.getByRole("checkbox", { name: "Can Show Voters", exact: true }).isChecked(), false);
    if (name === "Bob") {
        assert.equal(await admin.getByRole("checkbox", { name: "Can Complete/Reschedule", exact: true }).isChecked(), false);
    }
});

When("{word} opens Active Polls", async function (name) {
    const page = this.actors.get(name);
    await page.getByRole("link", { name: "Active Polls", exact: true }).click();
    await page.getByRole("heading", { name: "Active Polls", exact: true }).waitFor();
    await page.getByRole("heading", { name: "Add Poll", exact: true }).waitFor();
});

When("Bob creates a poll on a clear date", async function () {
    const bob = this.actors.get("Bob");
    assert.notEqual(bob.context(), this.actors.get("Claire").context());
    this.pollDate = await withEmulatorAdmin(nextAvailablePollDate);
    await bob.getByRole("link", { name: "Active Polls", exact: true }).click();
    await bob.getByRole("heading", { name: "Add Poll", exact: true }).waitFor();
    await bob.locator('input[type="date"]').fill(this.pollDate);
    await bob.getByRole("button", { name: "Add Poll", exact: true }).click();
    await bob.getByRole("heading", { name: this.pollDate, exact: true }).waitFor();
    const poll = await withEmulatorAdmin(async (db) => waitForValue(
        () => getDocsFromServer(query(collection(db, "polls"), where("date", "==", this.pollDate))),
        (polls) => polls.size === 1,
        `the poll on ${this.pollDate} to be committed`,
    ));
    this.pollId = poll.docs[0].id;
});

Then("{word} sees the poll on Active Polls", async function (name) {
    const page = this.actors.get(name);
    assert.equal(page.url(), `${BASE_URL}/active_polls`);
    await page.getByRole("heading", { name: this.pollDate, exact: true }).waitFor();
});

When("Bob filters the venue picker to Pub and adds:", async function (table) {
    const bob = this.actors.get("Bob");
    const poll = journeyPoll(this, "Bob");
    await poll.getByLabel("Filter by venue type:", { exact: true }).selectOption({ label: "Pub" });
    const picker = poll.getByRole("combobox").filter({
        has: bob.getByRole("option", { name: "Select a venue to add here", exact: true }),
    });
    for (const [name, type] of this.journeyVenueTypes) {
        if (type !== "pub") {
            assert.equal(await picker.getByRole("option", { name: this.journeyVenues.get(name), exact: true }).count(), 0);
        }
    }
    for (const { venue } of table.hashes()) {
        await picker.selectOption({ label: this.journeyVenues.get(venue) });
        await poll.getByRole("button", { name: "Add Venue To Poll", exact: true }).click();
        await venueRow(this, "Bob", venue).waitFor();
    }
});

Then("{word} sees the following pubs in the poll:", async function (name, table) {
    for (const { venue } of table.hashes()) {
        await venueRow(this, name, venue).waitFor();
        await venueRow(this, name, venue).getByRole("cell", { name: "0", exact: true }).waitFor();
    }
});

When("{word} votes for the following pubs:", async function (name, table) {
    for (const { venue } of table.hashes()) {
        const row = venueRow(this, name, venue);
        await row.getByRole("button", { name: "Respond", exact: true }).click();
        await row.getByRole("button", { name: "Vote Up", exact: true }).click();
        await row.getByRole("button", { name: "Respond", exact: true })
            .and(this.actors.get(name).locator('[aria-expanded="false"]')).waitFor();
    }
});

Then("{word} sees the following vote totals:", async function (name, table) {
    for (const { venue, votes } of table.hashes()) {
        await venueRow(this, name, venue).getByRole("cell", { name: votes, exact: true }).waitFor();
    }
});

When("Claire completes the poll by choosing {string}", async function (venue) {
    const claire = this.actors.get("Claire");
    const displayName = this.journeyVenues.get(venue);
    assert.equal(await venueRow(this, "Bob", venue).getByRole("button", { name: displayName, exact: true }).count(), 0);
    await venueRow(this, "Claire", venue).getByRole("button", { name: displayName, exact: true }).click();
    await claire.getByRole("heading", { name: "Complete Poll", exact: true }).waitFor();
    await withEmulatorAdmin((db) => waitForValue(async () => {
        const [request, ack] = await Promise.all([
            getDocFromServer(doc(db, "notification_req", this.pollId)),
            getDocFromServer(doc(db, "notification_ack", this.pollId)),
        ]);
        const requestedCompletion = request.data()?.complete;
        return requestedCompletion !== undefined && requestedCompletion === ack.data()?.complete;
    }, Boolean, "Last Orders completion acknowledgement"));
    await claire.getByRole("button", { name: "Confirm", exact: true }).click();
    await claire.getByRole("heading", { name: "Complete Poll", exact: true }).waitFor({ state: "hidden" });
});

Then("{word} no longer sees the poll on Active Polls", async function (name) {
    const page = this.actors.get(name);
    assert.equal(page.url(), `${BASE_URL}/active_polls`);
    await page.getByRole("heading", { name: this.pollDate, exact: true }).waitFor({ state: "hidden" });
});

When("{word} opens Current Events", async function (name) {
    const page = this.actors.get(name);
    await page.getByRole("link", { name: "Current Events", exact: true }).click();
    await page.getByRole("heading", { name: "Current Events", exact: true }).waitFor();
});

Then("{word} sees the meet at {string}", async function (name, venue) {
    const page = this.actors.get(name);
    const event = page.getByRole("article").filter({
        has: page.getByRole("heading", { name: this.journeyVenues.get(venue), exact: true }),
    });
    await event.getByText(this.pollDate, { exact: true }).waitFor();
});
