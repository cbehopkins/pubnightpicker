// @vitest-environment jsdom

import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const { getDocFromServerMock } = vi.hoisted(() => ({ getDocFromServerMock: vi.fn() }));
vi.mock("../../firebase", () => ({ db: {}, auth: {} }));
vi.mock("firebase/firestore", () => ({ doc: vi.fn(() => "deployment"), getDocFromServer: getDocFromServerMock }));

import BackendVersionPanel from "./BackendVersionPanel";

const identity = { tag: "release-v1.2.3", commit: "0123456789abcdef0123456789abcdef01234567" };
const snapshot = (data = identity) => ({ exists: () => true, metadata: { fromCache: false, hasPendingWrites: false }, data: () => data });
const response = (data = identity) => ({ ok: true, json: async () => data });

beforeEach(() => {
    getDocFromServerMock.mockReset().mockResolvedValue(snapshot());
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response()));
    vi.stubEnv("VITE_BUILD_TAG", "frontend-v1.2.3");
    vi.stubEnv("VITE_BUILD_COMMIT", identity.commit);
});

afterEach(() => { cleanup(); vi.useRealTimers(); vi.unstubAllGlobals(); vi.unstubAllEnvs(); });

describe("backend version diagnostics", () => {
    it("checks fresh identities on opening and shows a match", async () => {
        render(<BackendVersionPanel />);
        expect(await screen.findByText("Match")).toBeInTheDocument();
        expect(screen.getByText("Frontend").parentElement).toHaveTextContent("frontend-v1.2.3");
        expect(screen.getByText("Running backend").parentElement).toHaveTextContent(identity.commit);
        expect(fetch).toHaveBeenCalledWith("http://localhost:8081/api/version", expect.objectContaining({ method: "GET", cache: "no-store", credentials: "omit" }));
        expect(getDocFromServerMock).toHaveBeenCalledOnce();
    });

    it("shows local frontend Git information without a release build", async () => {
        vi.stubEnv("VITE_BUILD_TAG", "");
        vi.stubEnv("VITE_BUILD_COMMIT", "");
        vi.stubEnv("VITE_LOCAL_COMMIT", identity.commit);
        vi.stubEnv("VITE_LOCAL_MODIFIED", "true");
        render(<BackendVersionPanel />);
        await screen.findByText("Match");
        expect(screen.getByText("Frontend").parentElement).toHaveTextContent(identity.commit);
        expect(screen.getByText("Frontend").parentElement).toHaveTextContent("Working tree at build: Modified");
    });

    it("reports either tag or commit differences and refreshes", async () => {
        fetch.mockResolvedValueOnce(response({ ...identity, tag: "last_orders-v1.2.3" }));
        render(<BackendVersionPanel />);
        expect(await screen.findByText("Mismatch")).toBeInTheDocument();
        fetch.mockResolvedValueOnce(response({ ...identity, commit: "fedcba9876543210fedcba9876543210fedcba98" }));
        fireEvent.click(screen.getByRole("button", { name: "Refresh versions" }));
        expect(await screen.findByText("Mismatch")).toBeInTheDocument();
        expect(getDocFromServerMock).toHaveBeenCalledTimes(2);
    });

    it("reports unknown for missing, stale or unreachable identities", async () => {
        getDocFromServerMock.mockResolvedValueOnce({ ...snapshot(), metadata: { fromCache: true, hasPendingWrites: false } });
        fetch.mockRejectedValueOnce(new Error("offline"));
        render(<BackendVersionPanel />);
        expect(await screen.findByText("Unknown", { selector: "span" })).toBeInTheDocument();
        expect(screen.getByText("Unable to read the expected backend version.")).toBeInTheDocument();
        expect(screen.getByText("Backend not responding, or the browser blocked the request.")).toBeInTheDocument();
        expect(within(screen.getByText("Running backend").parentElement).getAllByText(/Unknown/)).toHaveLength(2);
    });

    it("keeps known fields visible when build metadata is incomplete", async () => {
        fetch.mockResolvedValueOnce(response({ tag: "", commit: identity.commit }));
        render(<BackendVersionPanel />);
        expect(await screen.findByText("Unknown", { selector: "span" })).toBeInTheDocument();
        expect(screen.getByText("Running backend").parentElement).toHaveTextContent(identity.commit);
        expect(screen.getByText("Running backend version is incomplete or invalid.")).toBeInTheDocument();
    });

    it("reports unknown when the backend request stalls", async () => {
        vi.useFakeTimers();
        fetch.mockReturnValueOnce(new Promise(() => { }));
        render(<BackendVersionPanel timeoutMs={100} />);
        await act(async () => { await vi.advanceTimersByTimeAsync(100); });
        expect(screen.getByText("Unknown", { selector: "span" })).toBeInTheDocument();
        expect(screen.getByText("Backend version check timed out.")).toBeInTheDocument();
        expect(fetch.mock.calls[0][1].signal.aborted).toBe(true);
    });

    it("identifies a connected development build and an unconfigured expectation", async () => {
        getDocFromServerMock.mockResolvedValueOnce({ exists: () => false, metadata: { fromCache: false, hasPendingWrites: false } });
        fetch.mockResolvedValueOnce(response({ tag: "", commit: identity.commit, build: "development", modified: true }));
        render(<BackendVersionPanel />);
        expect(await screen.findByText("Connected (development build)")).toBeInTheDocument();
        expect(screen.getByText("Not configured")).toBeInTheDocument();
        expect(screen.getByText("Running backend").parentElement).toHaveTextContent(identity.commit);
        expect(screen.getByText("Running backend").parentElement).toHaveTextContent("Working tree at build: Modified");
        expect(screen.queryByText("Running backend version is incomplete or invalid.")).not.toBeInTheDocument();
        expect(screen.getByRole("status")).toHaveTextContent("Unknown");
    });

    it("distinguishes an HTTP response without the endpoint from a network failure", async () => {
        fetch.mockResolvedValueOnce({ ok: false, status: 404 });
        render(<BackendVersionPanel />);
        expect(await screen.findByText("Backend responded with HTTP 404; version endpoint unavailable.")).toBeInTheDocument();
    });
});
