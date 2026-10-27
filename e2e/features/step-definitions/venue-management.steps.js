import { strict as assert } from "node:assert";
import { randomUUID } from "node:crypto";
import { When, Then } from "@cucumber/cucumber";

const BASE_URL = process.env.E2E_BASE_URL ?? "http://127.0.0.1:3000";
const TEXT_FIELDS = ["Name", "Address", "Venue Web Site", "Map", "Notes"];

async function fillVenue(page, details) {
    await page.getByLabel("Venue Type", { exact: true }).selectOption({ label: details["Venue Type"] });
    for (const field of TEXT_FIELDS) {
        await page.getByLabel(field, { exact: true }).fill(details[field]);
    }
    await page.getByRole("checkbox", { name: "Parking", exact: true }).setChecked(details.Parking === "true");
    if (details["Venue Type"] !== "Restaurant") {
        await page.getByRole("checkbox", { name: "Food", exact: true }).setChecked(details.Food === "true");
    }
}

async function saveVenue(page) {
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
    assert.equal(await page.getByRole("checkbox", { name: "Parking", exact: true }).isChecked(), this.venueDetails.Parking === "true");
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
    const pollDate = await page.evaluate(() => {
        const date = new Date();
        date.setDate(date.getDate() + 2);
        return [date.getFullYear(), String(date.getMonth() + 1).padStart(2, "0"), String(date.getDate()).padStart(2, "0")].join("-");
    });
    await page.getByRole("link", { name: "Active Polls", exact: true }).click();
    await page.getByRole("heading", { name: "Add Poll", exact: true }).waitFor();
    await page.locator('input[type="date"]').fill(pollDate);
    await page.getByRole("button", { name: "Add Poll", exact: true }).click();
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
    await poll.getByRole("button", { name: "Delete Poll", exact: true }).click();
    await heading.waitFor({ state: "hidden" });
});
