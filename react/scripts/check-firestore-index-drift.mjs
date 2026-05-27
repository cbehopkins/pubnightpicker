#!/usr/bin/env node

import { execSync } from "node:child_process";
import { readFileSync } from "node:fs";
import path from "node:path";

const ROOT = process.cwd();
const INDEX_FILE = path.join(ROOT, "firestore.indexes.json");

function readProjectIdFromFirebaserc(rootDir) {
    try {
        const firebasercRaw = readFileSync(path.join(rootDir, ".firebaserc"), "utf8");
        const firebaserc = JSON.parse(firebasercRaw);
        return firebaserc?.projects?.default ?? null;
    } catch {
        return null;
    }
}

function parseJson(content, sourceName) {
    try {
        return JSON.parse(content);
    } catch (error) {
        const message = error instanceof Error ? error.message : String(error);
        throw new Error(`Failed to parse ${sourceName}: ${message}`);
    }
}

function normalizeIndexField(field) {
    return {
        fieldPath: field.fieldPath,
        ...(field.order ? { order: field.order } : {}),
        ...(field.arrayConfig ? { arrayConfig: field.arrayConfig } : {}),
        ...(field.vectorConfig ? { vectorConfig: field.vectorConfig } : {}),
    };
}

function normalizeIndex(index) {
    const filteredFields = (index.fields ?? [])
        // Firebase often returns an explicit __name__ tiebreaker field.
        // It is implicit and not required in firestore.indexes.json definitions.
        .filter((field) => field.fieldPath !== "__name__")
        .map(normalizeIndexField);

    return {
        collectionGroup: index.collectionGroup,
        queryScope: index.queryScope,
        fields: filteredFields,
    };
}

function normalizeFieldOverrideIndex(index) {
    return {
        ...(index.order ? { order: index.order } : {}),
        ...(index.arrayConfig ? { arrayConfig: index.arrayConfig } : {}),
        ...(index.vectorConfig ? { vectorConfig: index.vectorConfig } : {}),
        ...(index.queryScope ? { queryScope: index.queryScope } : {}),
    };
}

function normalizeFieldOverride(override) {
    const indexes = (override.indexes ?? []).map(normalizeFieldOverrideIndex);
    const sortedIndexes = indexes.sort((a, b) =>
        JSON.stringify(a).localeCompare(JSON.stringify(b))
    );

    return {
        collectionGroup: override.collectionGroup,
        fieldPath: override.fieldPath,
        indexes: sortedIndexes,
    };
}

function normalizeSpec(spec) {
    const indexes = (spec.indexes ?? []).map(normalizeIndex);
    const fieldOverrides = (spec.fieldOverrides ?? []).map(normalizeFieldOverride);

    return {
        indexes: indexes.sort((a, b) => JSON.stringify(a).localeCompare(JSON.stringify(b))),
        fieldOverrides: fieldOverrides.sort((a, b) =>
            JSON.stringify(a).localeCompare(JSON.stringify(b))
        ),
    };
}

function toSignatureSet(entries) {
    return new Set(entries.map((entry) => JSON.stringify(entry)));
}

function diffEntries(source, target) {
    const sourceSet = toSignatureSet(source);
    const targetSet = toSignatureSet(target);

    const missing = source.filter((entry) => !targetSet.has(JSON.stringify(entry)));
    const extra = target.filter((entry) => !sourceSet.has(JSON.stringify(entry)));

    return { missing, extra };
}

function run() {
    const projectId =
        process.env.FIREBASE_PROJECT_ID ??
        process.env.VITE_FIREBASE_PROJECT_ID ??
        process.env.REACT_APP_FIREBASE_PROJECT_ID ??
        process.env.GCLOUD_PROJECT ??
        readProjectIdFromFirebaserc(ROOT);

    if (!projectId) {
        throw new Error(
            "Unable to determine Firebase project id. Set FIREBASE_PROJECT_ID or define .firebaserc projects.default."
        );
    }

    const localRaw = readFileSync(INDEX_FILE, "utf8");
    const localSpec = parseJson(localRaw, "firestore.indexes.json");

    const liveRaw = execSync(
        `npx --yes firebase-tools firestore:indexes --project ${JSON.stringify(projectId)}`,
        { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] }
    );

    const liveSpec = parseJson(liveRaw, "firebase firestore:indexes output");

    const normalizedLocal = normalizeSpec(localSpec);
    const normalizedLive = normalizeSpec(liveSpec);

    const indexDiff = diffEntries(normalizedLive.indexes, normalizedLocal.indexes);
    const overrideDiff = diffEntries(
        normalizedLive.fieldOverrides,
        normalizedLocal.fieldOverrides
    );

    const hasDiff =
        indexDiff.missing.length > 0 ||
        indexDiff.extra.length > 0 ||
        overrideDiff.missing.length > 0 ||
        overrideDiff.extra.length > 0;

    if (!hasDiff) {
        console.log("Firestore index drift check passed. Local file matches deployed indexes.");
        return;
    }

    console.error("Firestore index drift detected.");

    if (indexDiff.missing.length > 0) {
        console.error("\nIndexes present in project but missing from firestore.indexes.json:");
        for (const entry of indexDiff.missing) {
            console.error(JSON.stringify(entry, null, 2));
        }
    }

    if (indexDiff.extra.length > 0) {
        console.error("\nIndexes present in firestore.indexes.json but missing from project:");
        for (const entry of indexDiff.extra) {
            console.error(JSON.stringify(entry, null, 2));
        }
    }

    if (overrideDiff.missing.length > 0) {
        console.error("\nField overrides present in project but missing from firestore.indexes.json:");
        for (const entry of overrideDiff.missing) {
            console.error(JSON.stringify(entry, null, 2));
        }
    }

    if (overrideDiff.extra.length > 0) {
        console.error("\nField overrides present in firestore.indexes.json but missing from project:");
        for (const entry of overrideDiff.extra) {
            console.error(JSON.stringify(entry, null, 2));
        }
    }

    console.error(
        "\nResolve by reconciling firestore.indexes.json with deployed indexes, then re-run this check."
    );
    process.exitCode = 1;
}

try {
    run();
} catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    console.error(`Firestore index drift check failed: ${message}`);
    process.exitCode = 2;
}
