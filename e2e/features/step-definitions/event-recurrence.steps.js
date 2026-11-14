import { strict as assert } from "node:assert";
import { randomUUID } from "node:crypto";
import { Given, When, Then } from "@cucumber/cucumber";
import { collection, doc, getDocFromServer, getDocsFromServer, query, where } from "firebase/firestore";
import { fillVenue, saveVenue } from "./venue-management.steps.js";
import { displayDate, londonDate, recurrenceDates } from "../support/calendar.js";
import { waitForValue, withEmulatorAdmin } from "../support/emulator.js";

function assertSameLondonDay(world) {
    assert.equal(londonDate(), world.recurrenceDates.today, "London midnight crossed during this scenario; rerun with fresh data");
}

function venueId(world) {
    assert.match(world.journeyVenuePath, /^\/venues\/[^/]+$/);
    return decodeURIComponent(world.journeyVenuePath.split("/").at(-1));
}

async function configureRecurrence(page, frequency, date) {
    await page.getByLabel("Recurrence Type", { exact: true }).selectOption(frequency);
    if (frequency === "monthly") {
        await page.getByLabel("Fixed day (e.g., 15th of each month)", { exact: true }).check();
        await page.getByLabel("Day of Month", { exact: true }).fill(String(Number(date.slice(-2))));
    } else {
        const weekday = (new Date(`${date}T00:00:00Z`).getUTCDay() + 6) % 7;
        await page.getByLabel("Weekday", { exact: true }).selectOption(String(weekday));
        await page.getByLabel("Repeat Every (weeks)", { exact: true }).fill("1");
    }
}

Given("recurrence dates are anchored to today's London calendar date", function () {
    this.recurrenceDates = recurrenceDates();
});

When("{word} creates the journey Event with {string} recurrence", async function (name, frequency) {
    assert.ok(["monthly", "weekly"].includes(frequency));
    assertSameLondonDay(this);
    this.expectedOccurrence = this.recurrenceDates[frequency];
    this.venueDetails = {
        "Venue Type": "Event", Name: `Journey Recurrence ${randomUUID()}`,
        Address: "1 Journey Road, Cambridge", "Venue Web Site": "https://venue.example.test",
        Map: "https://maps.example.test/event", Notes: "Synthetic recurring event", Parking: "false", Food: "true",
    };
    const page = this.actors.get(name);
    await page.getByRole("link", { name: "Manage Venues", exact: true }).click();
    await page.getByRole("link", { name: "New Venue", exact: true }).click();
    await fillVenue(page, this.venueDetails);
    await configureRecurrence(page, frequency, this.expectedOccurrence);
    await saveVenue(page);
    await page.getByRole("link", { name: this.venueDetails.Name, exact: true }).waitFor();
    assertSameLondonDay(this);
});

Then("Last Orders persists the expected occurrence for the journey Event", { timeout: 60_000 }, async function () {
    const id = venueId(this);
    await withEmulatorAdmin(async (db, auth) => {
        await waitForValue(async () => {
            assertSameLondonDay(this);
            return getDocFromServer(doc(db, "pubs", id));
        }, (snapshot) => snapshot.exists() && snapshot.data().next_occurrence_date === this.expectedOccurrence,
            `venue ${id} next_occurrence_date=${this.expectedOccurrence}`);
        const token = await auth.currentUser.getIdToken();
        await waitForValue(async () => {
            const response = await fetch(new URL(`api/venues/${encodeURIComponent(id)}`, process.env.VITE_LAST_ORDERS_API_BASE_URL), {
                headers: { Authorization: `Bearer ${token}` },
            });
            const body = await response.text();
            assert.equal(response.status, 200, body);
            return JSON.parse(body);
        }, (venue) => venue.id === id && venue.next_occurrence_date === this.expectedOccurrence,
            `Last Orders projection for venue ${id} next_occurrence_date=${this.expectedOccurrence}`);
    });
    assertSameLondonDay(this);
});

Then("{word} sees the backend-computed Next Event in the venue form", async function (name) {
    const page = this.actors.get(name);
    assert.equal(new URL(page.url()).pathname, this.journeyVenuePath);
    await page.getByText("Next Event", { exact: true }).waitFor();
    await page.getByText(displayDate(this.expectedOccurrence), { exact: true }).waitFor();
    assertSameLondonDay(this);
});

When("{word} changes the journey Event to the other monthly date", async function (name) {
    assertSameLondonDay(this);
    this.expectedOccurrence = this.recurrenceDates.edited;
    const page = this.actors.get(name);
    assert.equal(new URL(page.url()).pathname, this.journeyVenuePath);
    await configureRecurrence(page, "monthly", this.expectedOccurrence);
    await saveVenue(page);
});

Then("Last Orders materialises the journey Event poll with votes and attendance", { timeout: 60_000 }, async function () {
    const id = venueId(this);
    await withEmulatorAdmin(async (db) => {
        const matches = await waitForValue(async () => {
            assertSameLondonDay(this);
            return getDocsFromServer(query(collection(db, "polls"), where("eventVenueId", "==", id), where("date", "==", this.expectedOccurrence)));
        }, (snapshot) => !snapshot.empty, `poll for venue ${id} on ${this.expectedOccurrence}`);
        assert.equal(matches.size, 1, "Expected one currently observed venue/occurrence poll; this is not a replay-idempotency test");
        const poll = matches.docs[0];
        this.recurrencePollId = poll.id;
        assert.equal(poll.data().eventOccurrenceDate, this.expectedOccurrence);
        assert.equal(poll.data().completed, false);
        assert.equal(poll.data().pubs[id].name, this.venueDetails.Name);
        for (const name of ["votes", "attendance"]) {
            await waitForValue(() => getDocFromServer(doc(db, name, poll.id)), (snapshot) => snapshot.exists(), `${name}/${poll.id}`);
        }
    });
    assertSameLondonDay(this);
});

Then("{word} sees the generated poll with the journey Event venue", async function (name) {
    assert.ok(this.recurrencePollId);
    const page = this.actors.get(name);
    const poll = page.getByRole("heading", { name: this.expectedOccurrence, exact: true }).locator("../..").filter({
        has: page.getByRole("row").filter({ hasText: this.venueDetails.Name }),
    });
    await poll.getByRole("row").filter({ hasText: this.venueDetails.Name }).waitFor();
    assertSameLondonDay(this);
});
