import { useEffect, useState } from "react";
import { doc, getDocFromServer } from "firebase/firestore";
import { db } from "../../firebase";
import { lastOrdersApiURL } from "../../utils/lastOrdersApi";

const releaseTag = /^(release-|last_orders-)?v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/;
const commitHash = /^[0-9a-f]{40}$/;

function validIdentity(value) {
    return value && typeof value.tag === "string" && releaseTag.test(value.tag)
        && typeof value.commit === "string" && commitHash.test(value.commit);
}

function identityFrom(value) {
    return {
        tag: typeof value?.tag === "string" && releaseTag.test(value.tag) ? value.tag : "",
        commit: typeof value?.commit === "string" && commitHash.test(value.commit) ? value.commit : "",
        build: value?.build === "development" || value?.build === "release" ? value.build : "",
        modified: typeof value?.modified === "boolean" ? value.modified : null,
    };
}

async function getExpectedVersion() {
    const snapshot = await getDocFromServer(doc(db, "config", "deployment"));
    if (snapshot.metadata.fromCache || snapshot.metadata.hasPendingWrites) {
        throw new Error("Expected backend version is unavailable or invalid.");
    }
    if (!snapshot.exists()) return null;
    return identityFrom(snapshot.data());
}

async function getActualVersion(signal) {
    let response;
    try {
        response = await fetch(lastOrdersApiURL("api/version"), {
            method: "GET", credentials: "omit", cache: "no-store", redirect: "error", signal,
        });
    } catch {
        throw new Error("Backend not responding, or the browser blocked the request.");
    }
    if (!response.ok) throw new Error(`Backend responded with HTTP ${response.status}; version endpoint unavailable.`);
    let data;
    try {
        data = await response.json();
    } catch {
        throw new Error("Backend responded with an invalid version response.");
    }
    if (!data || typeof data !== "object" || Array.isArray(data)) {
        throw new Error("Backend responded with an invalid version response.");
    }
    return identityFrom(data);
}

function Identity({ title, identity, detail }) {
    return (
        <div className="mb-2">
            <strong>{title}</strong>
            {detail && <div className="small">{detail}</div>}
            <div className="small text-break">Tag: {identity?.tag || "Unknown"}</div>
            <div className="small text-break">Commit: {identity?.commit || "Unknown"}</div>
            {identity?.modified != null && <div className="small">Working tree at build: {identity.modified ? "Modified" : "Clean"}</div>}
        </div>
    );
}

export default function BackendVersionPanel({ timeoutMs = 10000 }) {
    const [refresh, setRefresh] = useState(0);
    const [result, setResult] = useState({ checking: true, expected: null, actual: null, errors: [] });

    useEffect(() => {
        let active = true;
        const controller = new AbortController();
        setResult({ checking: true, expected: null, actual: null, errors: [] });
        let timer;
        const timeout = new Promise((_, reject) => {
            timer = setTimeout(() => {
                controller.abort();
                reject(new Error("Backend version check timed out."));
            }, timeoutMs);
        });
        Promise.allSettled([
            Promise.race([getExpectedVersion(), timeout]),
            Promise.race([getActualVersion(controller.signal), timeout]),
        ]).then(([expected, actual]) => {
            if (!active) return;
            setResult({
                checking: false,
                expected: expected.status === "fulfilled" ? expected.value : null,
                actual: actual.status === "fulfilled" ? actual.value : null,
                errors: [
                    ...(expected.status === "rejected" ? ["Unable to read the expected backend version."]
                        : expected.value && !validIdentity(expected.value) ? ["Expected backend version is incomplete or invalid."] : []),
                    ...(actual.status === "rejected" ? [actual.reason?.message || "Unable to read the running backend version."]
                        : actual.value.build !== "development" && !validIdentity(actual.value) ? ["Running backend version is incomplete or invalid."] : []),
                ],
            });
        }).finally(() => clearTimeout(timer));
        return () => {
            active = false;
            controller.abort();
            clearTimeout(timer);
        };
    }, [refresh, timeoutMs]);

    const { expected, actual, checking, errors } = result;
    const status = checking ? "Checking" : !validIdentity(expected) || !validIdentity(actual) || actual.build === "development" ? "Unknown"
        : expected.tag === actual.tag && expected.commit === actual.commit ? "Match" : "Mismatch";
    const badge = status === "Match" ? "bg-success" : status === "Mismatch" ? "bg-danger" : "bg-secondary";
    const frontendModified = import.meta.env.DEV && ["true", "false"].includes(import.meta.env.VITE_LOCAL_MODIFIED)
        ? import.meta.env.VITE_LOCAL_MODIFIED === "true" : null;
    const backendDetail = actual ? `Connected (${actual.build === "development" ? "development build" : actual.build === "release" ? "release build" : "build type unknown"})` : null;

    return (
        <section className="mb-4" aria-labelledby="backend-version-heading">
            <h2 id="backend-version-heading" className="h5 mb-2">Build versions</h2>
            <div className="d-flex align-items-center gap-2 mb-3">
                <button type="button" className="btn btn-outline-secondary btn-sm" disabled={checking} onClick={() => setRefresh((value) => value + 1)}>Refresh versions</button>
                <span className={`badge ${badge}`} role="status">{status}</span>
            </div>
            <Identity title="Frontend" identity={{ tag: import.meta.env.VITE_BUILD_TAG, commit: import.meta.env.VITE_BUILD_COMMIT || (import.meta.env.DEV ? import.meta.env.VITE_LOCAL_COMMIT : ""), modified: frontendModified }} detail={import.meta.env.DEV ? "Development server (working tree at server start)" : null} />
            <Identity title="Expected backend" identity={expected} detail={!checking && !expected && !errors.includes("Unable to read the expected backend version.") ? "Not configured" : null} />
            <Identity title="Running backend" identity={actual} detail={backendDetail} />
            {errors.map((error) => <p className="small text-danger mb-1" key={error}>{error}</p>)}
        </section>
    );
}
