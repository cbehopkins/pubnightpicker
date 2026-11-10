import { strict as assert } from "node:assert";
import { test } from "node:test";
import { displayDate, londonDate, recurrenceDates, shiftDate } from "./features/support/calendar.js";
import { waitForValue } from "./features/support/emulator.js";
import { createPushIdentity } from "./features/support/push-fixture.js";
import { parseCompleteBackendRecords } from "./features/support/backend-evidence.js";

test("London dates distinguish summer midnight from UTC midnight", () => {
    assert.equal(londonDate(new Date("2026-06-30T23:30:00Z")), "2026-07-01");
    assert.equal(londonDate(new Date("2026-12-31T23:30:00Z")), "2026-12-31");
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
