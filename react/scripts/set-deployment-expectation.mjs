#!/usr/bin/env node

import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import path from "node:path";
import { applicationDefault, deleteApp, initializeApp } from "firebase-admin/app";
import { getFirestore } from "firebase-admin/firestore";

const root = fileURLToPath(new URL("../../", import.meta.url));
const releaseTag = /^(release-|last_orders-)?v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/;
const commitHash = /^[0-9a-f]{40}$/;

export function parseArguments(args) {
    const options = { project: "", tag: "", apply: false };
    for (let index = 0; index < args.length; index += 1) {
        const argument = args[index];
        if (argument === "--apply" && !options.apply) {
            options.apply = true;
        } else if ((argument === "--project" || argument === "--tag") && !options[argument.slice(2)] && args[index + 1]) {
            options[argument.slice(2)] = args[++index];
        } else {
            throw new Error("Usage: node react/scripts/set-deployment-expectation.mjs --project PROJECT_ID --tag RELEASE_TAG [--apply]");
        }
    }
    if (!/^[a-z][a-z0-9-]{4,28}[a-z0-9]$/.test(options.project)) throw new Error("A valid explicit Firebase project ID is required.");
    if (!releaseTag.test(options.tag)) throw new Error("Tag must be a Last Orders release tag (release-vX.Y.Z, last_orders-vX.Y.Z or vX.Y.Z).");
    return options;
}

export function resolveRelease(tag, git) {
    const ref = `refs/tags/${tag}`;
    let local;
    let remote;
    try {
        local = git(["rev-parse", "--verify", `${ref}^{commit}`]).trim();
        remote = git(["ls-remote", "--exit-code", "origin", ref, `${ref}^{}`]);
    } catch {
        throw new Error("Could not resolve the local and origin release tag. Fetch the tag and check access to origin.");
    }
    const refs = new Map(remote.trim().split(/\r?\n/).map((line) => line.split(/\s+/)).map(([hash, name]) => [name, hash]));
    const remoteCommit = refs.get(`${ref}^{}`) ?? refs.get(ref);
    if (!commitHash.test(local) || !commitHash.test(remoteCommit ?? "") || local !== remoteCommit) {
        throw new Error("Local and origin tags do not resolve to the same commit.");
    }
    return { tag, commit: local };
}

export async function run(args, { git, write, log }) {
    const options = parseArguments(args);
    const identity = resolveRelease(options.tag, git);
    log(`Project: ${options.project}\nExpected backend: ${identity.tag}\nCommit: ${identity.commit}`);
    if (!options.apply) {
        log("Dry run only. Add --apply to write config/deployment before activation.");
        return;
    }
    await write(options.project, identity);
    log("Updated config/deployment. Activate the corresponding published image separately.");
}

export async function writeExpectation(projectId, identity) {
    const app = initializeApp({ projectId, credential: applicationDefault() });
    try {
        await getFirestore(app).doc("config/deployment").set(identity);
    } finally {
        await deleteApp(app);
    }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
    if (process.env.FIRESTORE_EMULATOR_HOST) console.log(`Firestore emulator: ${process.env.FIRESTORE_EMULATOR_HOST}`);
    run(process.argv.slice(2), {
        git: (args) => execFileSync("git", args, { cwd: root, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] }),
        write: writeExpectation,
        log: console.log,
    }).catch((error) => {
        console.error(error.message);
        process.exitCode = 1;
    });
}
