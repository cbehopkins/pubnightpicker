import { readFile } from "node:fs/promises";

import {
    assertFails,
    assertSucceeds,
    initializeTestEnvironment,
} from "@firebase/rules-unit-testing";
import { deleteDoc, doc, getDoc, setDoc, updateDoc } from "firebase/firestore";
import { afterAll, beforeAll, beforeEach, describe, it } from "vitest";

const PROJECT_ID = "pubnightpicker-poll-action-audit-rules";

let testEnv;

function validAuditCreate(overrides = {}) {
    return {
        pollId: "poll-1",
        actionType: "create",
        actorUid: "user-a",
        at: new Date("2026-05-15T12:00:00.000Z"),
        pollDate: "2026-05-16",
        ...overrides,
    };
}

function validVenueMutation(actionType, overrides = {}) {
    return validAuditCreate({
        actionType,
        selectedVenueId: "venue-1",
        venueName: "The Anchor",
        ...overrides,
    });
}

beforeAll(async () => {
    const rules = await readFile(new URL("../../firestore.rules", import.meta.url), "utf8");
    testEnv = await initializeTestEnvironment({
        projectId: PROJECT_ID,
        firestore: {
            host: "127.0.0.1",
            port: parseInt(process.env.VITEST_FIRESTORE_PORT ?? "8080"),
            rules,
        },
    });
});

afterAll(async () => {
    if (testEnv) {
        await testEnv.cleanup();
    }
});

beforeEach(async () => {
    await testEnv.clearFirestore();
    await testEnv.withSecurityRulesDisabled(async (context) => {
        const adminDb = context.firestore();
        await setDoc(doc(adminDb, "roles", "admin"), { "admin-user": true });
        await setDoc(doc(adminDb, "poll_action_audit", "seed-1"), {
            pollId: "poll-0",
            actionType: "create",
            actorUid: "someone",
            at: new Date("2026-05-10T12:00:00.000Z"),
            pollDate: "2026-05-10",
        });
    });
});

describe("poll_action_audit rules", () => {
    it("allows authenticated users to create their own actorUid records", async () => {
        const db = testEnv.authenticatedContext("user-a").firestore();
        await assertSucceeds(setDoc(doc(db, "poll_action_audit", "new-1"), validAuditCreate()));
    });

    it("denies create when actorUid does not match auth uid", async () => {
        const db = testEnv.authenticatedContext("user-a").firestore();
        await assertFails(
            setDoc(
                doc(db, "poll_action_audit", "new-2"),
                validAuditCreate({ actorUid: "user-b" })
            )
        );
    });

    it("denies unauthenticated create", async () => {
        const db = testEnv.unauthenticatedContext().firestore();
        await assertFails(setDoc(doc(db, "poll_action_audit", "new-3"), validAuditCreate()));
    });

    it("denies complete action without selectedVenueId", async () => {
        const db = testEnv.authenticatedContext("user-a").firestore();
        await assertFails(
            setDoc(
                doc(db, "poll_action_audit", "new-4"),
                validAuditCreate({ actionType: "complete" })
            )
        );
    });

    it("allows addVenue and deleteVenue with selectedVenueId", async () => {
        const db = testEnv.authenticatedContext("user-a").firestore();
        await assertSucceeds(
            setDoc(doc(db, "poll_action_audit", "new-5"), validVenueMutation("addVenue"))
        );
        await assertSucceeds(
            setDoc(doc(db, "poll_action_audit", "new-6"), validVenueMutation("deleteVenue"))
        );
    });

    it("denies addVenue and deleteVenue without selectedVenueId", async () => {
        const db = testEnv.authenticatedContext("user-a").firestore();
        await assertFails(
            setDoc(
                doc(db, "poll_action_audit", "new-7"),
                validAuditCreate({ actionType: "addVenue" })
            )
        );
        await assertFails(
            setDoc(
                doc(db, "poll_action_audit", "new-8"),
                validAuditCreate({ actionType: "deleteVenue" })
            )
        );
    });

    it("allows admin read", async () => {
        const db = testEnv.authenticatedContext("admin-user").firestore();
        await assertSucceeds(getDoc(doc(db, "poll_action_audit", "seed-1")));
    });

    it("denies non-admin read", async () => {
        const db = testEnv.authenticatedContext("user-a").firestore();
        await assertFails(getDoc(doc(db, "poll_action_audit", "seed-1")));
    });

    it("denies updates and deletes", async () => {
        const db = testEnv.authenticatedContext("admin-user").firestore();
        await assertFails(updateDoc(doc(db, "poll_action_audit", "seed-1"), { pollDate: "2026-05-11" }));
        await assertFails(deleteDoc(doc(db, "poll_action_audit", "seed-1")));
    });
});
