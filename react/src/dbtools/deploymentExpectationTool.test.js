import { describe, expect, it, vi } from "vitest";
import { parseArguments, resolveRelease, run } from "../../scripts/set-deployment-expectation.mjs";

const tag = "release-v1.2.3";
const commit = "0123456789abcdef0123456789abcdef01234567";
const ref = `refs/tags/${tag}`;
const args = ["--project", "pubnightpicker", "--tag", tag];

function gitFor(remoteCommit = commit) {
    return vi.fn((command) => command[0] === "rev-parse" ? `${commit}\n` : `aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\t${ref}\n${remoteCommit}\t${ref}^{}\n`);
}

describe("deployment expectation tool", () => {
    it("requires a project and a valid backend release tag", () => {
        expect(() => parseArguments(["--tag", tag])).toThrow();
        expect(() => parseArguments(["--project", "pubnightpicker", "--tag", "frontend-v1.2.3"])).toThrow();
        expect(() => parseArguments([...args, "--tag", tag])).toThrow();
        expect(parseArguments([...args, "--apply"])).toEqual({ project: "pubnightpicker", tag, apply: true });
    });

    it("resolves annotated and lightweight remote tags and rejects moved tags", () => {
        expect(resolveRelease(tag, gitFor())).toEqual({ tag, commit });
        expect(resolveRelease(tag, vi.fn((command) => command[0] === "rev-parse" ? commit : `${commit}\t${ref}\n`))).toEqual({ tag, commit });
        expect(() => resolveRelease(tag, gitFor("fedcba9876543210fedcba9876543210fedcba98"))).toThrow(/same commit/);
    });

    it("does not write on dry run and writes only the resolved identity when applied", async () => {
        const write = vi.fn();
        const options = { git: gitFor(), write, log: vi.fn() };
        await run(args, options);
        expect(write).not.toHaveBeenCalled();
        await run([...args, "--apply"], options);
        expect(write).toHaveBeenCalledWith("pubnightpicker", { tag, commit });
    });

    it("never writes when origin cannot be verified", async () => {
        const write = vi.fn();
        const git = vi.fn(() => { throw new Error("offline"); });
        await expect(run([...args, "--apply"], { git, write, log: vi.fn() })).rejects.toThrow(/Could not resolve/);
        expect(write).not.toHaveBeenCalled();
    });
});
