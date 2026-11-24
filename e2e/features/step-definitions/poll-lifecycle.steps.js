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
    updateDoc,
    where,
    writeBatch,
} from "firebase/firestore";
import { londonDate, londonInstant, shiftDate } from "../support/calendar.js";
import { waitForBackendRecord } from "../support/backend-evidence.js";
import { nextAvailablePollDate, waitForValue, withEmulatorAdmin, withEmulatorUser } from "../support/emulator.js";

const BASE_URL = process.env.E2E_BASE_URL ?? "http://127.0.0.1:3000";

async function captureVoteApi(page) {
    await page.evaluate(() => {
        if (window.__voteApiCaptureInstalled) return;
        window.__voteApiCaptureInstalled = true;
        window.__voteApiRequests = [];
        window.__voteApiResponses = "";
        window.__attendanceApiRequests = [];
        window.__attendanceApiReadRequests = [];
        window.__attendanceApiResponses = "";
        const nativeFetch = window.fetch.bind(window);
        window.fetch = async (input, init = {}) => {
            const url = input instanceof Request ? input.url : String(input);
            const method = String(init.method || (input instanceof Request ? input.method : "GET")).toUpperCase();
            const response = await nativeFetch(input, init);
            if (/\/api\/meet\/[^/]+\/votes(?:\/[^/?]+)?(?:\?.*)?$/.test(url) || /\/api\/meets\/[^/]+\/votes\/stream\?/.test(url)) {
                window.__voteApiRequests.push({ url, method });
                const reader = response.clone().body?.getReader();
                if (reader) {
                    void (async () => {
                        const decoder = new TextDecoder();
                        while (true) {
                            const { done, value } = await reader.read();
                            if (done) break;
                            window.__voteApiResponses += decoder.decode(value, { stream: true });
                        }
                    })().catch(() => undefined);
                }
            }
            if (/\/api\/meet\/[^/]+\/attendance\/[^/?]+(?:\?.*)?$/.test(url)) {
                window.__attendanceApiRequests.push({ url, method, body: init.body, status: response.status });
            }
            if (method === "GET" && (/\/api\/meet\/[^/]+\/attendance(?:\?.*)?$/.test(url)
                || /\/api\/meets\/[^/]+\/attendance\/stream(?:\?.*)?$/.test(url))) {
                window.__attendanceApiReadRequests.push({ url, method, status: response.status });
            }
            if (/\/api\/meets\/[^/]+\/attendance\/stream(?:\?.*)?$/.test(url)) {
                const reader = response.clone().body?.getReader();
                if (reader) {
                    void (async () => {
                        const decoder = new TextDecoder();
                        while (true) {
                            const { done, value } = await reader.read();
                            if (done) break;
                            window.__attendanceApiResponses += decoder.decode(value, { stream: true });
                        }
                    })().catch(() => undefined);
                }
            }
            return response;
        };
    });
}

function journeyPoll(world, actor, date = world.pollDate) {
    return world.actors.get(actor)
        .getByRole("heading", { name: date, exact: true })
        .locator("../..");
}

function venueRow(world, actor, venue, date = world.pollDate) {
    return journeyPoll(world, actor, date).getByRole("row")
        .filter({ hasText: world.journeyVenues.get(venue) });
}

function currentMeetCard(world, actor, venue) {
    const page = world.actors.get(actor);
    return page.getByRole("article").filter({
        has: page.getByRole("heading", { name: world.journeyVenues.get(venue), exact: true }),
    });
}

async function closeAttendanceModal(world, viewerName) {
    const close = world.actors.get(viewerName).getByRole("button", { name: "Close", exact: true });
    if (await close.count() > 0) await close.click();
}

async function attendanceCell(world, viewerName, userName, venue, status) {
    const viewer = world.actors.get(viewerName);
    const card = currentMeetCard(world, viewerName, venue);
    const table = viewer.getByRole("table");
    if (await table.count() === 0) {
        await card.getByRole("button", { name: "Show venue attendance", exact: true }).click();
    }
    const headers = await table.locator("thead th").allTextContents();
    const columnIndex = headers.indexOf(status);
    if (columnIndex === -1) return null;
    const row = table.getByRole("row").filter({
        has: viewer.getByRole("cell", { name: userName, exact: true }),
    });
    return row.getByRole("cell").nth(columnIndex);
}

async function adminAttendanceCell(world, userName, venue, status) {
    return attendanceCell(world, "admin", userName, venue, status);
}

function applicationTimeDate(dayOffset) {
    const initialInstant = process.env.LAST_ORDERS_E2E_INITIAL_TIME;
    assert.ok(initialInstant, "the E2E application clock must have an initial instant");
    return shiftDate(londonDate(new Date(initialInstant)), dayOffset);
}

function applicationTimePollID(world, dayOffset) {
    const date = applicationTimeDate(dayOffset);
    const pollID = world.applicationTimePolls?.get(date);
    assert.ok(pollID, `the application-time poll for ${date} must exist`);
    return pollID;
}

async function createPoll(world, date) {
    const bob = world.actors.get("Bob");
    await captureVoteApi(bob);
    world.pollDate = date;
    assert.notEqual(bob.context(), world.actors.get("Claire")?.context());
    await bob.getByRole("link", { name: "Active Polls", exact: true }).click();
    await bob.getByRole("heading", { name: "Add Poll", exact: true }).waitFor();
    await bob.locator('input[type="date"]').fill(world.pollDate);
    await bob.getByRole("button", { name: "Add Poll", exact: true }).click();
    await bob.getByRole("heading", { name: world.pollDate, exact: true }).waitFor();
    const poll = await withEmulatorAdmin((db) => waitForValue(
        () => getDocsFromServer(query(collection(db, "polls"), where("date", "==", world.pollDate))),
        (polls) => polls.size === 1,
        `the poll on ${world.pollDate} to be committed`,
    ));
    world.pollId = poll.docs[0].id;
    world.applicationTimePolls ??= new Map();
    world.applicationTimePolls.set(date, world.pollId);
}

async function assertPollRemainsOpen(pollID) {
    const poll = await withEmulatorAdmin((db) => getDocFromServer(doc(db, "polls", pollID)));
    assert.equal(poll.data()?.completed, false, "voting must remain open before the scheduled deadline");
    assert.equal(poll.data()?.selected ?? null, null, "an open poll must not have an automatic winner");
}

async function selectedVenueID(world, venue) {
    const venueSnapshot = await withEmulatorAdmin((db) => getDocsFromServer(query(
        collection(db, "pubs"),
        where("name", "==", world.journeyVenues.get(venue)),
    )));
    assert.equal(venueSnapshot.size, 1, "the selected venue must be uniquely identified");
    return venueSnapshot.docs[0].id;
}

async function waitForPollCompletion(world, pollID, venue) {
    const expectedVenueID = await selectedVenueID(world, venue);
    const poll = await withEmulatorAdmin((db) => waitForValue(
        () => getDocFromServer(doc(db, "polls", pollID)),
        (snapshot) => snapshot.data()?.completed === true,
        "Last Orders to complete the poll after its London deadline",
    ));
    assert.equal(poll.data()?.selected, expectedVenueID);
}

Given("the application-time processing floor matches the supplied London date", function () {
    const initialInstant = process.env.LAST_ORDERS_E2E_INITIAL_TIME;
    assert.ok(initialInstant, "the E2E application clock must have an initial instant");
    assert.equal(
        process.env.LAST_ORDERS_E2E_POLLS_SINCE,
        shiftDate(londonDate(new Date(initialInstant)), -1),
        "the processing floor must include yesterday's catch-up fixture",
    );
});

Given("startup autocomplete discovery has settled before fixture creation", async function () {
    const startupDate = shiftDate(applicationTimeDate(0), -1);
    await waitForBackendRecord(
        (record) => record.msg === "auto-completion discovery found eligible polls"
            && record.observed_on === startupDate
            && record.count === 0,
        `startup auto-completion discovery through ${startupDate} to settle`,
    );
});

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

Given("Bob and Claire become members of the default Group", async function () {
    assert.equal(process.env.GOOGLE_CLOUD_PROJECT, "demo-pubnightpicker-journey");
    assert.equal(process.env.FIRESTORE_EMULATOR_HOST, "127.0.0.1:8080");
    const [bobUid, claireUid] = await Promise.all(["Bob", "Claire"].map((name) => withEmulatorUser(
        this.actorEmails.get(name),
        "journey-only-user-password-2026",
        async (auth) => auth.currentUser.uid,
    )));
    const groupsResponse = await fetch("http://127.0.0.1:8081/api/groups?name=ampubnight");
    assert.equal(groupsResponse.status, 200, "Last Orders must return the default Group");
    const { groups } = await groupsResponse.json();
    assert.equal(groups.length, 1, "the default Group must exist exactly once");
    const projectID = process.env.GOOGLE_CLOUD_PROJECT;
    const documentURL = `http://127.0.0.1:8080/v1/projects/${projectID}/databases/(default)/documents/groups/${groups[0].id}`;
    const existingResponse = await fetch(documentURL, { headers: { Authorization: "Bearer owner" } });
    assert.equal(existingResponse.status, 200, "the Group fixture must be readable by the emulator owner");
    const existingGroup = await existingResponse.json();
    const members = { ...existingGroup.fields?.members?.mapValue?.fields };
    members[bobUid] = { booleanValue: true };
    members[claireUid] = { booleanValue: true };
    const updateResponse = await fetch(`${documentURL}?updateMask.fieldPaths=members`, {
        method: "PATCH",
        headers: { Authorization: "Bearer owner", "Content-Type": "application/json" },
        body: JSON.stringify({ fields: { members: { mapValue: { fields: members } } } }),
    });
    assert.equal(updateResponse.status, 200, `emulator Group membership update failed: ${await updateResponse.text()}`);
    this.actorUids ??= new Map();
    this.actorUids.set("Bob", bobUid);
    this.actorUids.set("Claire", claireUid);
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

When("Claire hides her votes from other members", async function () {
    const profile = await withEmulatorAdmin((db) => getDocsFromServer(query(
        collection(db, "user-public"),
        where("name", "==", "Claire"),
    )));
    assert.equal(profile.size, 1, "Claire must have one public profile in the Auth/Firestore emulators");
    this.hiddenVoteUid = profile.docs[0].id;
    await withEmulatorAdmin((db) => updateDoc(doc(db, "user-public", this.hiddenVoteUid), { votesVisible: false }));
});

When("{word} opens Active Polls", async function (name) {
    const page = this.actors.get(name);
    await captureVoteApi(page);
    await page.getByRole("link", { name: "Active Polls", exact: true }).click();
    await page.getByRole("heading", { name: "Active Polls", exact: true }).waitFor();
    await page.getByRole("heading", { name: "Add Poll", exact: true }).waitFor();
});

When("Bob creates a poll on a clear date", async function () {
    const date = await withEmulatorAdmin(nextAvailablePollDate);
    await createPoll(this, date);
});

When("Bob creates a poll for the frozen application date", async function () {
    await createPoll(this, applicationTimeDate(0));
});

When("Bob creates a poll for application-time day {int}", async function (dayOffset) {
    assert.ok(dayOffset >= -2 && dayOffset <= 2, "application-time day offset must be between -2 and 2");
    await createPoll(this, applicationTimeDate(dayOffset));
});

Then("{word} sees the poll on Active Polls", async function (name) {
    const page = this.actors.get(name);
    assert.equal(page.url(), `${BASE_URL}/active_polls`);
    await page.getByRole("heading", { name: this.pollDate, exact: true }).waitFor();
});

Then("the admin sees Bob as the poll creator in the audit", async function () {
    const admin = this.actors.get("admin");
    assert.notEqual(admin.context(), this.actors.get("Bob").context());
    await admin.getByRole("link", { name: "Admin", exact: true }).click();
    await admin.getByRole("button", { name: "Logs", exact: true }).click();
    await admin.getByRole("heading", { name: "Poll Action Audit", exact: true }).waitFor();
    await admin.getByLabel("Action", { exact: true }).selectOption({ label: "Create" });
    const row = admin.getByRole("row").filter({
        has: admin.getByText(this.pollId, { exact: true }),
    });
    await row.getByRole("cell", { name: "create", exact: true }).waitFor();
    assert.equal(await row.count(), 1, "the poll must have exactly one creation audit row");
    await row.getByText(this.pollDate, { exact: true }).waitFor();
    await row.getByRole("cell").getByText("Bob", { exact: true }).waitFor();
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

Then("Bob sees the vote through the API without Claire's UID", async function () {
    const bob = this.actors.get("Bob");
    const claire = this.actors.get("Claire");
    const votePath = `/api/meet/${this.pollId}/votes/`;
    await bob.waitForFunction(() => window.__voteApiResponses?.length > 0);
    const bobTraffic = await bob.evaluate(() => ({
        requests: window.__voteApiRequests,
        responses: window.__voteApiResponses,
    }));
    const claireRequests = await claire.evaluate(() => window.__voteApiRequests);
    assert.ok(claireRequests.some(({ url, method }) => url.includes(votePath) && method === "PUT"), `Claire vote requests: ${JSON.stringify(claireRequests)}`);
    assert.ok(bobTraffic.responses.includes('"anonymous_count":1'), `expected an anonymous vote in API responses: ${bobTraffic.responses}`);
    assert.ok(bobTraffic.requests.some(({ url }) => {
        const parsed = new URL(url);
        return parsed.pathname.endsWith("/votes/stream") && parsed.searchParams.getAll("meet_id").includes(this.pollId);
    }), `Bob vote requests: ${JSON.stringify(bobTraffic.requests)}`);
    assert.ok(bobTraffic.requests.some(({ url, method }) => url.endsWith(votePath.slice(0, -1)) && method === "GET"));
    assert.ok(!bobTraffic.responses.includes(this.hiddenVoteUid), "the hidden voter UID must not reach Bob's browser");
});

Then("Claire receives Bob's public voter summary through the API", async function () {
    const claire = this.actors.get("Claire");
    await claire.waitForFunction(() => window.__voteApiResponses?.includes('"name":"Bob"'));
    const responses = await claire.evaluate(() => window.__voteApiResponses);
    assert.ok(!responses.includes(this.hiddenVoteUid), "the hidden voter UID must not appear in voting API responses");
    assert.ok(!responses.includes('"name":"Claire"'), "the hidden voter name must not appear in voting API responses");
});

When("the application clock advances to {string} London time", async function (localTime) {
    const instant = londonInstant(this.pollDate, localTime);
    const acknowledgement = await this.advanceApplicationTime(instant);
    assert.equal(Date.parse(acknowledgement.instant), Date.parse(instant));
    assert.equal(acknowledgement.scheduling_notified, true);
});

Then("the application-time poll remains open", async function () {
    await assertPollRemainsOpen(this.pollId);
});

Then("the application-time destination London date is day {int}", function (dayOffset) {
    assert.equal(this.pollDate, applicationTimeDate(dayOffset));
});

Then("the application-time poll for day {int} remains open", async function (dayOffset) {
    await assertPollRemainsOpen(applicationTimePollID(this, dayOffset));
});

Then("Last Orders completes the application-time poll at {string}", async function (venue) {
    await waitForPollCompletion(this, this.pollId, venue);
});

Then("Last Orders completes application-time day {int} at {string}", async function (dayOffset, venue) {
    await waitForPollCompletion(this, applicationTimePollID(this, dayOffset), venue);
});

Then("Last Orders leaves the application-time poll for manual completion due to a tie", async function () {
    await waitForBackendRecord(
        (record) => record.msg === "poll requires manual completion"
            && record.poll_id === this.pollId
            && record.reason === "tie",
        "Last Orders to classify the tied poll for manual completion",
    );
    await assertPollRemainsOpen(this.pollId);
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

When("{word} opens Current Meets", async function (name) {
    const page = this.actors.get(name);
    await captureVoteApi(page);
    await page.getByRole("link", { name: "Current Meets", exact: true }).click();
    await page.waitForURL(`${BASE_URL}/current_meets`);
    await page.getByRole("heading", { name: "Current Meets", exact: true }).waitFor();
});

Then("{word} sees the meet at {string}", async function (name, venue) {
    const page = this.actors.get(name);
    const event = page.getByRole("article").filter({
        has: page.getByRole("heading", { name: this.journeyVenues.get(venue), exact: true }),
    });
    await event.getByText(this.pollDate, { exact: true }).waitFor();
});

When("{word} marks {string} as {string}", async function (name, venue, status) {
    const card = currentMeetCard(this, name, venue);
    await card.getByRole("button", { name: status, exact: true }).click();
    const selectedLabel = status === "Can come" ? "Can come confirmed" : "Cannot come confirmed";
    try {
        await card.getByRole("button", { name: selectedLabel, exact: true }).waitFor();
    } catch (error) {
        const requests = await this.actors.get(name).evaluate(() => window.__attendanceApiRequests ?? []);
        const reads = await this.actors.get(name).evaluate(() => window.__attendanceApiReadRequests ?? []);
        const stream = await this.actors.get(name).evaluate(() => (window.__attendanceApiResponses ?? "").slice(-2000));
        throw new Error(`${error.message}; attendance API reads: ${JSON.stringify(reads.slice(-3))}; writes: ${JSON.stringify(requests.slice(-3))}; stream tail: ${stream}`);
    }
});

When("{word} adds ETA {string} at {string}", async function (name, eta, venue) {
    const card = currentMeetCard(this, name, venue);
    await card.getByRole("button", { name: "Add ETA", exact: true }).click();
    await card.locator('input[type="time"]').fill(eta);
    await card.getByRole("button", { name: "Save", exact: true }).click();
    await card.getByText(`ETA: ${eta}`, { exact: true }).waitFor();
});

Then("the admin sees {string} can come to {string}", async function (userName, venue) {
    const status = "Can Come";
    const cell = await adminAttendanceCell(this, userName, venue, status);
    assert.ok(cell, `attendance table must include ${status}`);
    await cell.getByText("✓", { exact: true }).waitFor();
    await closeAttendanceModal(this, "admin");
});

Then("the admin sees {string} cannot come to {string}", async function (userName, venue) {
    const status = "Cannot Come";
    const cell = await adminAttendanceCell(this, userName, venue, status);
    assert.ok(cell, `attendance table must include ${status}`);
    await cell.getByText("✓", { exact: true }).waitFor();
    await closeAttendanceModal(this, "admin");
});

Then("{word} sees {word} can come to {string}", async function (viewerName, userName, venue) {
    const cell = await attendanceCell(this, viewerName, userName, venue, "Can Come");
    assert.ok(cell, "attendance table must include Can Come");
    await cell.getByText("✓", { exact: true }).waitFor();
    await closeAttendanceModal(this, viewerName);
});

Then("{word} sees {word} cannot come to {string}", async function (viewerName, userName, venue) {
    const cell = await attendanceCell(this, viewerName, userName, venue, "Cannot Come");
    assert.ok(cell, "attendance table must include Cannot Come");
    await cell.getByText("✓", { exact: true }).waitFor();
    await closeAttendanceModal(this, viewerName);
});

Then("{word} sees {word} has no attendance response for {string}", async function (viewerName, userName, venue) {
    const canComeCell = await attendanceCell(this, viewerName, userName, venue, "Can Come");
    const cannotComeCell = await attendanceCell(this, viewerName, userName, venue, "Cannot Come");
    if (canComeCell) await canComeCell.getByText("✓", { exact: true }).waitFor({ state: "detached" });
    if (cannotComeCell) await cannotComeCell.getByText("✓", { exact: true }).waitFor({ state: "detached" });
    await closeAttendanceModal(this, viewerName);
});

Then("Claire's browser used the attendance API to load the live view", async function () {
    const page = this.actors.get("Claire");
    await page.waitForFunction((meetID) => {
        const requests = window.__attendanceApiReadRequests ?? [];
        return requests.some(({ url }) => new URL(url).pathname === `/api/meet/${meetID}/attendance`)
            && requests.some(({ url }) => new URL(url).pathname.endsWith("/attendance/stream"));
    }, this.pollId);
});

When("Claire reconnects to Current Meets", async function () {
    const page = this.actors.get("Claire");
    await page.reload();
    await page.getByRole("heading", { name: "Current Meets", exact: true }).waitFor();
    await currentMeetCard(this, "Claire", "Journey Red Lion").waitFor();
});

Then("changing Bob's response clears only Bob's ETA", async function () {
    const venueID = await selectedVenueID(this, "Journey Red Lion");
    const snapshot = await withEmulatorAdmin((db) => waitForValue(
        () => getDocFromServer(doc(db, "attendance", this.pollId)),
        (value) => value.data()?.[venueID]?.cannotCome?.includes(this.actorUids.get("Bob")),
        "Bob's cannot-come response to commit",
    ));
    const etas = snapshot.data()?.[venueID]?.eta ?? {};
    assert.equal(etas[this.actorUids.get("Bob")], undefined, "Bob's ETA must be cleared");
    assert.equal(etas[this.actorUids.get("Claire")], "19:00", "Claire's ETA must be preserved");
});

When("Bob withdraws attendance for {string}", async function (venue) {
    await currentMeetCard(this, "Bob", venue).getByRole("button", { name: "Clear response", exact: true }).click();
    await currentMeetCard(this, "Bob", venue).getByRole("button", { name: "Can come", exact: true }).waitFor();
});

Then("the admin sees {string} has no attendance response for {string}", async function (userName, venue) {
    const canComeCell = await adminAttendanceCell(this, userName, venue, "Can Come");
    const cannotComeCell = await adminAttendanceCell(this, userName, venue, "Cannot Come");
    if (canComeCell) await canComeCell.getByText("✓", { exact: true }).waitFor({ state: "detached" });
    if (cannotComeCell) await cannotComeCell.getByText("✓", { exact: true }).waitFor({ state: "detached" });
    await closeAttendanceModal(this, "admin");
});

Then("{word} still sees {string} as {string}", async function (name, venue, label) {
    await currentMeetCard(this, name, venue).getByRole("button", { name: label, exact: true }).waitFor();
});

Then("Bob's browser used the attendance API to set and withdraw responses for {string}", async function (venue) {
    const venueID = await selectedVenueID(this, venue);
    const requests = await this.actors.get("Bob").evaluate(() => window.__attendanceApiRequests ?? []);
    const venueRequests = requests.filter(({ url }) => new URL(url).pathname.endsWith(`/attendance/${venueID}`));
    assert.deepEqual(venueRequests.map(({ method, body }) => [method, body]), [
        ["PUT", JSON.stringify({ status: "attending" })],
        ["PUT", JSON.stringify({ status: "not_attending" })],
        ["DELETE", undefined],
    ]);
});
