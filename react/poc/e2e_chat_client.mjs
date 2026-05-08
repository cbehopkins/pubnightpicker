#!/usr/bin/env node

import { initializeApp } from "firebase/app";
import {
    connectAuthEmulator,
    createUserWithEmailAndPassword,
    getAuth,
    signInWithEmailAndPassword,
} from "firebase/auth";
import {
    addDoc,
    collection,
    connectFirestoreEmulator,
    getFirestore,
    serverTimestamp,
} from "firebase/firestore";

function parseFirestoreHost(value) {
    const host = String(value || "127.0.0.1:8180");
    const idx = host.lastIndexOf(":");
    if (idx <= 0) {
        throw new Error(`Invalid FIRESTORE_EMULATOR_HOST: ${host}`);
    }
    return {
        hostname: host.slice(0, idx),
        port: Number(host.slice(idx + 1)),
    };
}

function projectId() {
    return (
        process.env.GOOGLE_CLOUD_PROJECT
        || process.env.FIREBASE_PROJECT_ID
        || "demo-firebase-sub-integration"
    );
}

async function ensureUser(auth, email, password) {
    try {
        const created = await createUserWithEmailAndPassword(auth, email, password);
        return created.user;
    } catch (error) {
        if (error?.code !== "auth/email-already-in-use") {
            throw error;
        }
    }

    const signedIn = await signInWithEmailAndPassword(auth, email, password);
    return signedIn.user;
}

function buildSdk() {
    const app = initializeApp({
        apiKey: "demo-api-key",
        authDomain: `${projectId()}.firebaseapp.com`,
        projectId: projectId(),
    });

    const auth = getAuth(app);
    const db = getFirestore(app);

    const authHost = process.env.FIREBASE_AUTH_EMULATOR_HOST || "127.0.0.1:9199";
    const fs = parseFirestoreHost(process.env.FIRESTORE_EMULATOR_HOST);

    connectAuthEmulator(auth, `http://${authHost}`, { disableWarnings: true });
    connectFirestoreEmulator(db, fs.hostname, fs.port);

    return { auth, db };
}

async function cmdSignup({ auth }, args) {
    const [email, password] = args;
    if (!email || !password) {
        throw new Error("signup requires: <email> <password>");
    }

    const user = await ensureUser(auth, email, password);
    process.stdout.write(`${JSON.stringify({ uid: user.uid, email: user.email })}\n`);
}

async function cmdSendMessage({ auth, db }, args) {
    const [email, password, name, text, scopeTypeArg, scopeIdArg] = args;
    if (!email || !password || !name || !text) {
        throw new Error(
            "send-message requires: <email> <password> <name> <text> [scopeType] [scopeId]"
        );
    }

    const scopeType = scopeTypeArg || "global";
    const scopeId = scopeIdArg || (scopeType === "event" ? "poll-1" : "main");

    const user = await ensureUser(auth, email, password);

    const messageRef = await addDoc(collection(db, "messages"), {
        uid: user.uid,
        name,
        text,
        createdAt: serverTimestamp(),
        scopeType,
        scopeId,
    });

    process.stdout.write(
        `${JSON.stringify({ uid: user.uid, messageId: messageRef.id, scopeType, scopeId })}\n`
    );
}

async function main() {
    const [command, ...rest] = process.argv.slice(2);
    if (!command) {
        throw new Error("Missing command. Use: signup | send-message");
    }

    const sdk = buildSdk();

    if (command === "signup") {
        await cmdSignup(sdk, rest);
        return;
    }
    if (command === "send-message") {
        await cmdSendMessage(sdk, rest);
        return;
    }

    throw new Error(`Unknown command: ${command}`);
}

main().catch((error) => {
    const payload = { error: error?.message || String(error) };
    process.stderr.write(`${JSON.stringify(payload)}\n`);
    process.exit(1);
});
