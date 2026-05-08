#!/usr/bin/env node
/**
 * E2E Frontend Test Client
 *
 * Posts a global chat message to the Firestore emulator.
 * Usage: node frontend-client.js [messageText]
 * Outputs: JSON with messageId and timestamp
 */

import { initializeApp } from "firebase/app";
import {
    connectAuthEmulator,
    getAuth,
    signInWithEmailAndPassword,
} from "firebase/auth";
import {
    connectFirestoreEmulator,
    getFirestore,
    serverTimestamp,
    addDoc,
    collection,
} from "firebase/firestore";

const FIRESTORE_EMULATOR_HOST = "127.0.0.1:8180";
const AUTH_EMULATOR_HOST = "127.0.0.1:9199";
const PROJECT_ID = "demo-firebase-sub-integration";

const firebaseConfig = {
    apiKey: "AIzaSyDHLfVs5P5p5PkWOLgFiQJXy9cNpTq7YUU", // fake key for emulator
    projectId: PROJECT_ID,
    storageBucket: `${PROJECT_ID}.appspot.com`,
    appId: "1:123456789:web:abcdef1234567890",
};

async function main() {
    try {
        // Initialize Firebase
        const app = initializeApp(firebaseConfig);
        const auth = getAuth(app);
        const firestore = getFirestore(app);

        // Connect to emulators
        connectAuthEmulator(auth, `http://${AUTH_EMULATOR_HOST}`);
        connectFirestoreEmulator(firestore, ...FIRESTORE_EMULATOR_HOST.split(":"));

        // Sign in as smoke-user-a so smoke-user-b is an eligible recipient.
        const email = "smoke-user-a@test.local";
        const password = "test-password-a";

        try {
            await signInWithEmailAndPassword(auth, email, password);
        } catch (error) {
            if (error.code === "auth/user-not-found") {
                // User might not exist; this is expected during first run
                // The orchestrator should have seeded the user
                console.error(
                    `Failed to sign in as ${email}: user not found. Did orchestrator seed users?`
                );
                process.exit(1);
            }
            throw error;
        }

        const user = auth.currentUser;
        if (!user) {
            throw new Error("User not authenticated");
        }

        // Post a global chat message
        const messageText = process.argv[2] || "E2E test message";
        const messagesRef = collection(firestore, "messages");

        const docRef = await addDoc(messagesRef, {
            scopeType: "global",
            scopeId: "main",
            uid: user.uid,
            displayName: "Smoke User A",
            name: "Smoke User A",
            text: messageText,
            createdAt: serverTimestamp(),
            updatedAt: serverTimestamp(),
        });

        // Output result as JSON for orchestrator to parse
        console.log(
            JSON.stringify({
                success: true,
                messageId: docRef.id,
                uid: user.uid,
                timestamp: new Date().toISOString(),
            })
        );

        process.exit(0);
    } catch (error) {
        console.error(
            JSON.stringify({
                success: false,
                error: error.message,
            })
        );
        process.exit(1);
    }
}

main();
