import { strict as assert } from "node:assert";
import { test } from "node:test";
import { displayDate, londonDate, londonInstant, recurrenceDates, shiftDate } from "./features/support/calendar.js";
import { advanceApplicationTime } from "./features/support/application-clock.js";
import { waitForValue } from "./features/support/emulator.js";
import { createPushIdentity } from "./features/support/push-fixture.js";
import { parseCompleteBackendRecords } from "./features/support/backend-evidence.js";
import { createServer } from "node:http";

test("London dates distinguish summer midnight from UTC midnight", () => {
    assert.equal(londonDate(new Date("2026-06-30T23:30:00Z")), "2026-07-01");
    assert.equal(londonDate(new Date("2026-12-31T23:30:00Z")), "2026-12-31");
});

test("London local instants use the date's daylight-saving offset", () => {
    assert.equal(londonInstant("2026-07-04", "16:00"), "2026-07-04T16:00:00+01:00");
    assert.equal(londonInstant("2026-12-04", "16:00"), "2026-12-04T16:00:00+00:00");
});

test("recurrence targets cross month, leap-day and year boundaries", () => {
    const dates = recurrenceDates(new Date("2026-12-31T12:00:00Z"));
    assert.equal(dates.today, "2026-12-31");
    assert.equal(dates.monthly, "2027-01-10");
    assert.equal(dates.edited, "2027-01-12");
    assert.equal(dates.weekly, "2027-01-01");
    assert.equal(shiftDate("2028-02-28", 1), "2028-02-29");
    assert.equal(shiftDate("2028-02-29", 1), "2028-03-01");
    assert.equal(shiftDate("2026-10-24", 2), "2026-10-26");
});

test("Next Event display uses ordinal calendar dates", () => {
    assert.equal(displayDate("2027-01-01"), "1st January 2027");
    assert.equal(displayDate("2027-01-12"), "12th January 2027");
    assert.equal(displayDate("2027-01-23"), "23rd January 2027");
});

test("condition waits return the observed matching value", async () => {
    assert.equal(await waitForValue(async () => "ready", (value) => value === "ready", "synthetic value"), "ready");
});

test("a stalled read is bounded and retains its assertion context", async () => {
    await assert.rejects(waitForValue(() => new Promise(() => { }), () => false, "synthetic stalled read", 20), /synthetic stalled read/);
});

test("synthetic push identities are unique and have serialisable browser keys", () => {
    const first = createPushIdentity();
    const second = createPushIdentity();
    assert.notEqual(first.endpoint, second.endpoint);
    assert.equal(new URL(first.endpoint).hostname, "push.example.test");
    assert.equal(Buffer.from(first.keys.p256dh, "base64url").length, 65);
    assert.equal(Buffer.from(first.keys.p256dh, "base64url")[0], 4);
    assert.equal(Buffer.from(first.keys.auth, "base64url").length, 16);
    assert.equal(JSON.parse(JSON.stringify(first)).endpoint, first.endpoint);
});

test("backend JSONL parsing ignores only a partial final record", () => {
    const records = parseCompleteBackendRecords('{"msg":"ready"}\n{"msg":"partial"}');
    assert.equal(records.length, 1);
    assert.equal(records[0].msg, "ready");
    assert.throws(() => parseCompleteBackendRecords('{"msg":"ready"}\nnot-json\n'), /Malformed complete backend JSONL record at line 2/);
    assert.throws(() => parseCompleteBackendRecords('[]\n'), /is not an object/);
});

test("application clock helper sends advances only to its loopback endpoint", async () => {
    const server = createServer((request, response) => {
        assert.equal(request.method, "POST");
        assert.equal(request.url, "/clock");
        let body = "";
        request.setEncoding("utf8");
        request.on("data", (chunk) => { body += chunk; });
        request.on("end", () => {
            assert.deepEqual(JSON.parse(body), { instant: "2026-07-04T16:00:00+01:00" });
            response.setHeader("content-type", "application/json");
            response.end(JSON.stringify({ instant: "2026-07-04T16:00:00+01:00", scheduling_notified: true }));
        });
    });
    await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
    try {
        const address = server.address();
        const result = await advanceApplicationTime("2026-07-04T16:00:00+01:00", `http://127.0.0.1:${address.port}`);
        assert.equal(result.scheduling_notified, true);
        await assert.rejects(advanceApplicationTime("2026-07-04T16:00:00Z", "https://clock.example.test"), /loopback/);
    } finally {
        await new Promise((resolve, reject) => server.close((error) => error ? reject(error) : resolve()));
    }
});
