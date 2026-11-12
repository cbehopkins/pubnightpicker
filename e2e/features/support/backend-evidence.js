import { readFile } from "node:fs/promises";
import { waitForValue } from "./emulator.js";

export function parseCompleteBackendRecords(contents) {
    const lastNewline = contents.lastIndexOf("\n");
    if (lastNewline < 0) return [];
    const complete = contents.slice(0, lastNewline + 1);
    return complete.split(/\r?\n/).flatMap((line, index) => {
        if (!line.trim()) return [];
        let record;
        try {
            record = JSON.parse(line);
        } catch (error) {
            throw new Error(`Malformed complete backend JSONL record at line ${index + 1}`, { cause: error });
        }
        if (!record || typeof record !== "object" || Array.isArray(record)) {
            throw new Error(`Backend JSONL record at line ${index + 1} is not an object`);
        }
        return [record];
    });
}

export async function readBackendRecords(path = process.env.E2E_BACKEND_LOG_PATH) {
    if (!path) throw new Error("E2E_BACKEND_LOG_PATH is required for backend evidence");
    return parseCompleteBackendRecords(await readFile(path, "utf8"));
}

export async function waitForBackendRecord(matches, description, timeout = 20_000) {
    const records = await waitForValue(
        readBackendRecords,
        (current) => current.some(matches),
        description,
        timeout,
    );
    return records.find(matches);
}
