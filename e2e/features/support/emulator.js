import { strict as assert } from "node:assert";
import { randomUUID } from "node:crypto";
import { setTimeout } from "node:timers/promises";
import { initializeApp, deleteApp } from "firebase/app";
import { getAuth, connectAuthEmulator, signInWithEmailAndPassword } from "firebase/auth";
import { collection, connectFirestoreEmulator, getDocsFromServer, getFirestore, terminate } from "firebase/firestore";
import { londonDate, shiftDate } from "./calendar.js";

export async function withEmulatorAdmin(operation) {
    assert.equal(process.env.GOOGLE_CLOUD_PROJECT, "demo-pubnightpicker-journey");
    assert.equal(process.env.FIRESTORE_EMULATOR_HOST, "127.0.0.1:8080");
    assert.equal(process.env.FIREBASE_AUTH_EMULATOR_HOST, "127.0.0.1:9099");
    const app = initializeApp({ apiKey: "journey-emulator-key", projectId: process.env.GOOGLE_CLOUD_PROJECT }, `journey-reader-${randomUUID()}`);
    const auth = getAuth(app);
    connectAuthEmulator(auth, "http://127.0.0.1:9099", { disableWarnings: true });
    const db = getFirestore(app);
    connectFirestoreEmulator(db, "127.0.0.1", 8080);
    try {
        await signInWithEmailAndPassword(auth, process.env.E2E_ADMIN_EMAIL, process.env.E2E_ADMIN_PASSWORD);
        return await operation(db, auth);
    } finally {
        try {
            await terminate(db);
        } finally {
            await deleteApp(app);
        }
    }
}

export async function nextAvailablePollDate(db) {
    const snapshot = await getDocsFromServer(collection(db, "polls"));
    const usedDates = new Set(snapshot.docs.map((poll) => poll.data().date).filter((date) => typeof date === "string"));
    let date = shiftDate(londonDate(), 1);
    while (usedDates.has(date)) {
        date = shiftDate(date, 1);
    }
    return date;
}

export async function waitForValue(read, matches, description, timeout = 20_000) {
    const deadline = Date.now() + timeout;
    while (Date.now() < deadline) {
        const controller = new AbortController();
        let value;
        try {
            value = await Promise.race([
                read(),
                setTimeout(Math.max(1, deadline - Date.now()), undefined, { signal: controller.signal }).then(() => {
                    throw new Error(`Timed out waiting for ${description}`);
                }),
            ]);
        } finally {
            controller.abort();
        }
        if (matches(value)) return value;
        await setTimeout(100);
    }
    throw new Error(`Timed out waiting for ${description}`);
}
