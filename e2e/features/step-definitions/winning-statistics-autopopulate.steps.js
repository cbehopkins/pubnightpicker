import { strict as assert } from "node:assert";
import { randomUUID } from "node:crypto";
import { When, Then } from "@cucumber/cucumber";
import {
    collection,
    doc,
    getDocsFromServer,
    query,
    where,
    writeBatch,
} from "firebase/firestore";
import { londonDate, shiftDate } from "../support/calendar.js";
import { waitForValue, withEmulatorAdmin } from "../support/emulator.js";

const BASE_URL = process.env.E2E_BASE_URL ?? "http://127.0.0.1:3000";

async function seedWinningHistory(world) {
    assert.equal(process.env.GOOGLE_CLOUD_PROJECT, "demo-pubnightpicker-journey");
    assert.equal(process.env.FIRESTORE_EMULATOR_HOST, "127.0.0.1:8080");
    assert.equal(process.env.FIREBASE_AUTH_EMULATOR_HOST, "127.0.0.1:9099");
    assert.ok(world.defaultGroupID, "the default Group must be discovered before seeding history");

    const fixtureID = randomUUID();
    const venueIDs = new Map();
    await withEmulatorAdmin(async (db) => {
        for (const key of ["Statistics Crown", "Statistics Red Lion", "Statistics Zero Winner"]) {
            const snapshot = await getDocsFromServer(query(
                collection(db, "pubs"),
                where("name", "==", world.journeyVenues.get(key)),
            ));
            assert.equal(snapshot.size, 1, `${key} must be uniquely seeded`);
            venueIDs.set(key, snapshot.docs[0].id);
        }

        const today = londonDate();
        const crowns = [shiftDate(today, -60), shiftDate(today, -30)];
        const redLion = shiftDate(today, -42);
        const history = [
            { groupID: world.defaultGroupID, venue: "Statistics Crown", date: crowns[0] },
            { groupID: world.defaultGroupID, venue: "Statistics Crown", date: crowns[1] },
            { groupID: world.defaultGroupID, venue: "Statistics Red Lion", date: redLion },
            // This control would raise Crown's count from two to eleven if the API crossed Groups.
            ...Array.from({ length: 9 }, (_, index) => ({
                groupID: `other-group-${fixtureID}`,
                venue: "Statistics Crown",
                date: shiftDate(today, -20 + index),
            })),
        ];
        const batch = writeBatch(db);
        for (const [index, item] of history.entries()) {
            const venueID = venueIDs.get(item.venue);
            batch.set(doc(db, "polls", `winning-stats-${fixtureID}-${index}`), {
                groupId: item.groupID,
                date: item.date,
                completed: true,
                selected: venueID,
                pubs: { [venueID]: { name: world.journeyVenues.get(item.venue) } },
            });
        }
        await batch.commit();
        world.winningStatsFixture = {
            venueIDs,
            crownLastDate: crowns[1],
            redLionLastDate: redLion,
        };
    });
}

When("the winning statistics history fixture is seeded for the current Group", async function () {
    await seedWinningHistory(this);
});

When("the admin opens winning venue statistics", async function () {
    const admin = this.actors.get("admin");
    this.winningStatsResponse = admin.waitForResponse((response) => {
        const url = new URL(response.url());
        return url.pathname === `/api/groups/${encodeURIComponent(this.defaultGroupID)}/winning-statistics`
            && response.request().method() === "GET";
    });
    this.winningStatsResponse.catch(() => undefined);
    // A wide configured limit keeps fixtures visible in both ranked lists even
    // when earlier scenarios have added other Venues to the shared emulator.
    await admin.goto(`${BASE_URL}/stats/winning_venues?limit=100&years=1`);
    await admin.getByRole("heading", { name: "Winning Venue Stats", exact: true }).waitFor();
});

Then("the admin sees accurate Group-scoped winning statistics", async function () {
    const response = await this.winningStatsResponse;
    assert.equal(response.status(), 200, "the Last Orders statistics request must succeed");
    const report = await response.json();
    const { venueIDs, crownLastDate, redLionLastDate } = this.winningStatsFixture;
    assert.equal(report.group_id, this.defaultGroupID);
    assert.equal(report.counts_by_venue_id[venueIDs.get("Statistics Crown")], 2);
    assert.equal(report.last_date_by_venue_id[venueIDs.get("Statistics Crown")], crownLastDate);
    assert.equal(report.counts_by_venue_id[venueIDs.get("Statistics Red Lion")], 1);
    assert.equal(report.last_date_by_venue_id[venueIDs.get("Statistics Red Lion")], redLionLastDate);
    assert.equal(report.counts_by_venue_id[venueIDs.get("Statistics Zero Winner")], undefined);

    const admin = this.actors.get("admin");
    await waitForValue(
        async () => admin.getByRole("listitem").allTextContents(),
        (items) => items.some((text) => text.includes(this.journeyVenues.get("Statistics Crown"))),
        "the winning statistics report to render its ranked Venue rows",
    );
    for (const [venueKey, count, lastDate] of [
        ["Statistics Crown", "2", crownLastDate],
        ["Statistics Red Lion", "1", redLionLastDate],
        ["Statistics Zero Winner", "0", null],
    ]) {
        const displayName = this.journeyVenues.get(venueKey);
        const matchingRows = admin.getByRole("listitem").filter({
            has: admin.getByText(displayName, { exact: true }),
        });
        assert.equal(await matchingRows.count(), 2,
            `${displayName} must be shown in both ranking lists with the configured wide limit`);
        for (const row of await matchingRows.all()) {
            assert.equal((await row.locator(".text-end .fw-semibold").textContent())?.trim(), count,
                `${displayName} must show exactly ${count} win(s)`);
            if (lastDate) {
                await row.getByText(`Last win ${lastDate}`, { exact: true }).waitFor();
            } else {
                assert.equal(await row.getByText(/Last win/).count(), 0,
                    "a zero-win Venue has no last-win date");
            }
        }
    }
    assert.deepEqual(
        Object.keys(report.counts_by_venue_id).sort(),
        [venueIDs.get("Statistics Crown"), venueIDs.get("Statistics Red Lion")].sort(),
        "the API must include only winners from the selected Group",
    );
});

When("Bob autopopulates the poll", async function () {
    const bob = this.actors.get("Bob");
    const poll = bob.getByRole("heading", { name: this.pollDate, exact: true }).locator("../..");
    const button = poll.getByRole("button", { name: "Autopopulate", exact: true });
    await button.waitFor();
    await button.click();
    for (const key of ["Statistics Crown", "Statistics Red Lion", "Statistics Zero Winner"]) {
        await poll.getByRole("row").filter({ hasText: this.journeyVenues.get(key) }).waitFor();
    }
    const audits = await withEmulatorAdmin((db) => getDocsFromServer(query(
        collection(db, "poll_action_audit"),
        where("pollId", "==", this.pollId),
    )));
    const additions = audits.docs.map((snapshot) => snapshot.data())
        .filter((entry) => entry.actionType === "addVenue");
    assert.equal(additions.length, 3, "each of the three actual additions must have one audit row");
    assert.ok(additions.every((entry) => entry.actorUid === this.actorUids.get("Bob")));
});

Then("another admin sees the autopopulate audit attributed to Bob", async function () {
    const admin = this.actors.get("admin");
    const bob = this.actors.get("Bob");
    assert.notEqual(admin.context(), bob.context(), "the audit must be checked in a separate administrator session");
    await admin.getByRole("link", { name: "Admin", exact: true }).click();
    await admin.getByRole("button", { name: "Logs", exact: true }).click();
    await admin.getByRole("heading", { name: "Poll Action Audit", exact: true }).waitFor();
    await admin.getByLabel("Action", { exact: true }).selectOption({ label: "Add venue" });
    const rows = admin.getByRole("row").filter({
        has: admin.getByText(this.pollId, { exact: true }),
    });
    await waitForValue(() => rows.count(), (count) => count === 3,
        "the admin Logs page to show all three autopopulate audit rows");
    for (const row of await rows.all()) {
        await row.getByRole("cell", { name: "addVenue", exact: true }).waitFor();
        await row.getByRole("cell").getByText("Bob", { exact: true }).waitFor();
    }
});
