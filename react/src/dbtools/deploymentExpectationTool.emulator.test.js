import { describe, expect, it } from "vitest";
import { deleteApp, initializeApp } from "firebase-admin/app";
import { getFirestore } from "firebase-admin/firestore";
import { writeExpectation } from "../../scripts/set-deployment-expectation.mjs";

describe("deployment expectation Firestore integration", () => {
    it("writes the exact expectation to the Firestore emulator", async () => {
        const emulatorHost = process.env.FIRESTORE_EMULATOR_HOST;
        if (!/^(localhost|127\.0\.0\.1):\d+$/.test(emulatorHost ?? "")) {
            throw new Error("FIRESTORE_EMULATOR_HOST must point to a local Firestore emulator");
        }

        const projectId = "deployment-expectation-test";
        const app = initializeApp({ projectId }, "expectation-test-reader");
        try {
            await writeExpectation(projectId, { tag: "release-v1.2.3", commit: "0123456789abcdef0123456789abcdef01234567" });
            const result = await getFirestore(app).doc("config/deployment").get();
            expect(result.data()).toEqual({ tag: "release-v1.2.3", commit: "0123456789abcdef0123456789abcdef01234567" });
        } finally {
            await deleteApp(app);
        }
    }, 30000);
});