import { strict as assert } from "node:assert";
import { randomUUID } from "node:crypto";
import { Given, When, Then } from "@cucumber/cucumber";
import { doc, setDoc } from "firebase/firestore";
import { nextAvailablePollDate, withEmulatorAdmin } from "../support/emulator.js";
import { londonDate, shiftDate } from "../support/calendar.js";

const BASE_URL = process.env.E2E_BASE_URL ?? "http://127.0.0.1:3000";
const TEXT_FIELDS = ["Name", "Address", "Venue Web Site", "Map", "Notes"];

Given("a venue is available to current and past events", async function () {
    const fixtureId = randomUUID();
    this.publicVenueId = `public-access-${fixtureId}`;
    this.publicVenueName = `Public Access Venue ${fixtureId}`;
    const today = londonDate();

    await withEmulatorAdmin(async (db, auth) => {
        const venueResponse = await fetch(new URL("api/venues", process.env.VITE_LAST_ORDERS_API_BASE_URL), {
            method: "POST",
            headers: {
                Authorization: `Bearer ${await auth.currentUser.getIdToken()}`,
                "Content-Type": "application/json",
            },
            body: JSON.stringify({
                name: this.publicVenueName,
                venue_type: "pub",
                address: "Public Access Road",
            }),
        });
        const venueBody = await venueResponse.text();
        assert.equal(venueResponse.status, 201, venueBody);
        this.publicVenueId = JSON.parse(venueBody).id;

        await Promise.all([
            setDoc(doc(db, "polls", `public-current-${fixtureId}`), {
                date: today,
                completed: true,
                selected: this.publicVenueId,
            }),
            setDoc(doc(db, "polls", `public-past-${fixtureId}`), {
                date: shiftDate(today, -1),
                completed: true,
                selected: this.publicVenueId,
            }),
        ]);
    });
});

When("{word} views the public venue in Current Events", async function (name) {
    const page = this.actors.get(name) ?? await this.openActor(name);
    await page.goto(`${BASE_URL}/current_events`);
    await page.getByRole("heading", { name: "Current Events", exact: true }).waitFor();
});

Then("{word} sees the public venue details in Current Events", async function (name) {
    const page = this.actors.get(name);
    await page.getByText(this.publicVenueName, { exact: true }).waitFor();
    await page.getByText("Public Access Road", { exact: true }).first().waitFor();
});

When("{word} views the public venue in Past Events", async function (name) {
    const page = this.actors.get(name);
    await page.goto(`${BASE_URL}/past_events`);
    await page.getByRole("heading", { name: "Past Events", exact: true }).waitFor();
});

Then("{word} sees the public venue in Past Events", async function (name) {
    const page = this.actors.get(name);
    await page.getByText(this.publicVenueName, { exact: true }).waitFor();
});

When("{word} views the venue catalogue without management permission", async function (name) {
    const page = this.actors.get(name);
    await page.goto(`${BASE_URL}/venues`);
});

Then("{word} sees the public venue without management actions", async function (name) {
    const page = this.actors.get(name);
    await page.getByText(this.publicVenueName, { exact: true }).waitFor();
    await page.getByRole("link", { name: "New Venue", exact: true }).waitFor({ state: "hidden" });
    await page.getByRole("button", { name: "Delete", exact: true }).waitFor({ state: "hidden" });
    assert.equal(await page.getByRole("link", { name: this.publicVenueName, exact: true }).count(), 0);
});

export async function fillVenue(page, details) {
    await page.getByLabel("Venue Type", { exact: true }).selectOption({ label: details["Venue Type"] });
    for (const field of TEXT_FIELDS) {
        await page.getByLabel(field, { exact: true }).fill(details[field]);
    }
    for (const field of ["Parking", "Dog Friendly", "Beer Garden", "Out Of Town (Cambridge)", "Banned"]) {
        await page.getByRole("checkbox", { name: field, exact: true }).setChecked(details[field] === "true");
    }
    if (details["Venue Type"] !== "Restaurant") {
        await page.getByRole("checkbox", { name: "Food", exact: true }).setChecked(details.Food === "true");
    }
}

export async function saveVenue(page) {
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await page.waitForURL(`${BASE_URL}/venues`);
}

When("{word} opens Manage Venues", async function (name) {
    const page = this.actors.get(name);
    await page.getByRole("link", { name: "Manage Venues", exact: true }).click();
    await page.getByRole("link", { name: "New Venue", exact: true }).waitFor();
    await page.getByLabel("Filter by venue type:", { exact: true }).selectOption("all");
});

When("{word} creates a {string} venue with these details:", async function (name, type, table) {
    const page = this.actors.get(name);
    this.venueSuffix = randomUUID();
    this.venueDetails = { ...table.rowsHash(), "Venue Type": type };
    this.venueDetails.Name = `${this.venueDetails.Name} ${this.venueSuffix}`;
    await page.getByRole("link", { name: "Manage Venues", exact: true }).click();
    await page.getByRole("link", { name: "New Venue", exact: true }).click();
    await fillVenue(page, this.venueDetails);
    await saveVenue(page);
    await page.getByRole("link", { name: this.venueDetails.Name, exact: true }).waitFor();
});

Then("{word} sees the journey venue listed as {string}", async function (name, type) {
    const page = this.actors.get(name);
    await page.getByLabel("Filter by venue type:", { exact: true }).selectOption({ label: type });
    await page.getByRole("link", { name: this.venueDetails.Name, exact: true }).waitFor();
});

When("{word} opens the journey venue for editing", async function (name) {
    const page = this.actors.get(name);
    await page.getByRole("link", { name: this.venueDetails.Name, exact: true }).click();
    await page.getByLabel("Name", { exact: true }).waitFor();
    const venuePath = new URL(page.url()).pathname;
    assert.match(venuePath, /^\/venues\/[^/]+$/);
    assert.notEqual(venuePath, "/venues/new");
    if (this.journeyVenuePath) {
        assert.equal(venuePath, this.journeyVenuePath, "Editing must preserve the venue identity");
    } else {
        this.journeyVenuePath = venuePath;
    }
});

Then("{word} sees the saved journey venue details", async function (name) {
    const page = this.actors.get(name);
    for (const field of TEXT_FIELDS) {
        assert.equal(await page.getByLabel(field, { exact: true }).inputValue(), this.venueDetails[field], field);
    }
    const selectedType = await page.getByLabel("Venue Type", { exact: true }).locator("option:checked").innerText();
    assert.equal(selectedType, this.venueDetails["Venue Type"]);
    for (const field of ["Parking", "Dog Friendly", "Beer Garden", "Out Of Town (Cambridge)", "Banned"]) {
        assert.equal(await page.getByRole("checkbox", { name: field, exact: true }).isChecked(), this.venueDetails[field] === "true", field);
    }
    if (selectedType === "Restaurant") {
        assert.equal(await page.getByRole("checkbox", { name: "Food", exact: true }).count(), 0);
    } else {
        assert.equal(await page.getByRole("checkbox", { name: "Food", exact: true }).isChecked(), this.venueDetails.Food === "true");
    }
});

When("{word} updates the journey venue with these details:", async function (name, table) {
    const page = this.actors.get(name);
    this.originalVenueName = this.venueDetails.Name;
    this.venueDetails = { ...this.venueDetails, ...table.rowsHash() };
    this.venueDetails.Name = `${this.venueDetails.Name} ${this.venueSuffix}`;
    await fillVenue(page, this.venueDetails);
    await saveVenue(page);
    await page.getByRole("link", { name: this.venueDetails.Name, exact: true }).waitFor();
});

Then("{word} sees the renamed journey venue without reloading", async function (name) {
    const page = this.actors.get(name);
    assert.equal(page.url(), `${BASE_URL}/venues`);
    await page.getByRole("link", { name: this.venueDetails.Name, exact: true }).waitFor();
    await page.getByRole("link", { name: this.originalVenueName, exact: true }).waitFor({ state: "hidden" });
});

When("{word} changes the journey venue type to {string}", async function (name, type) {
    const page = this.actors.get(name);
    await page.getByLabel("Venue Type", { exact: true }).selectOption({ label: type });
    this.venueDetails["Venue Type"] = type;
    if (type === "Restaurant") {
        this.venueDetails.Food = "true";
    }
    await saveVenue(page);
    await page.getByRole("link", { name: this.venueDetails.Name, exact: true }).waitFor();
});

Then("{word} no longer sees the journey venue under {string}", async function (name, type) {
    const page = this.actors.get(name);
    assert.equal(page.url(), `${BASE_URL}/venues`);
    await page.getByLabel("Filter by venue type:", { exact: true }).selectOption({ label: type });
    await page.getByRole("link", { name: this.venueDetails.Name, exact: true }).waitFor({ state: "hidden" });
});

Then("{word} can select the edited journey venue in the poll picker", async function (name) {
    const page = this.actors.get(name);
    const pollDate = await withEmulatorAdmin(nextAvailablePollDate);
    await page.getByRole("link", { name: "Active Polls", exact: true }).click();
    await page.getByRole("heading", { name: "Add Poll", exact: true }).waitFor();
    await page.locator('input[type="date"]').fill(pollDate);
    const addPollButton = page.getByRole("button", { name: "Add Poll", exact: true });
    await page.waitForFunction((date) => {
        const input = document.querySelector('input[type="date"]');
        const button = [...document.querySelectorAll("button")].find((item) => item.textContent?.trim() === "Add Poll");
        return input?.value === date && button && !button.disabled;
    }, pollDate);
    await addPollButton.click();
    const heading = page.getByRole("heading", { name: pollDate, exact: true });
    await heading.waitFor();
    const poll = heading.locator("../..");
    await poll.getByLabel("Filter by venue type:", { exact: true }).selectOption({ label: this.venueDetails["Venue Type"] });
    const picker = poll.getByRole("combobox").filter({
        has: page.getByRole("option", { name: "Select a venue to add here", exact: true }),
    });
    await picker.selectOption({ label: this.venueDetails.Name });
    assert.equal(await picker.getByRole("option", { name: this.originalVenueName, exact: true }).count(), 0);
    assert.equal(await picker.locator("option:checked").innerText(), this.venueDetails.Name);
    await poll.getByRole("button", { name: "Add Venue To Poll", exact: true }).click();
    await page.getByRole("alert").getByText(/marked as banned/i).waitFor();
    await poll.getByRole("button", { name: "Delete Poll", exact: true }).click();
    await heading.waitFor({ state: "hidden" });
});

When("{word} deletes the journey venue", async function (name) {
    const page = this.actors.get(name);
    await page.getByRole("link", { name: "Manage Venues", exact: true }).click();
    const venueLink = page.getByRole("link", { name: this.venueDetails.Name, exact: true });
    await venueLink.waitFor();
    await venueLink.locator("..").getByRole("button", { name: "Delete", exact: true }).click();
    await venueLink.waitFor({ state: "hidden" });
});

Then("{word} no longer sees the journey venue without reloading", async function (name) {
    const page = this.actors.get(name);
    assert.equal(page.url(), `${BASE_URL}/venues`);
    await page.getByRole("link", { name: this.venueDetails.Name, exact: true }).waitFor({ state: "hidden" });
});
