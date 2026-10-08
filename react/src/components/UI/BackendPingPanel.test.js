// @vitest-environment jsdom

import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const { authMock, getIdTokenMock, authListeners } = vi.hoisted(() => ({
    authMock: { currentUser: null },
    getIdTokenMock: vi.fn(),
    authListeners: new Set(),
}));

vi.mock("../../firebase", () => ({ auth: authMock }));
vi.mock("firebase/auth", () => ({
    onAuthStateChanged: vi.fn((_auth, listener) => {
        authListeners.add(listener);
        return () => authListeners.delete(listener);
    }),
}));

import BackendPingPanel from "./BackendPingPanel";
import EmailHistoryPanel from "./EmailHistoryPanel";
import { lastOrdersApiURL, requestLastOrders } from "../../utils/lastOrdersApi";

const requestID = "bca1207e-0519-4512-b9b5-c1b8a1d6fd00";
const fetchMock = vi.fn();
const jsonResponse = (data, status = 200) => ({
    status, ok: status >= 200 && status < 300,
    headers: new Headers({ "Content-Type": "application/json" }),
    json: async () => data,
});
const success = () => jsonResponse({ status: "ok", uid: "signed-in-user", request_id: requestID });

const historySuccess = (entries = []) => jsonResponse({ status: "ok", uid: "signed-in-user", request_id: requestID, entries });

describe("EmailHistoryPanel", () => {
    it("loads authenticated history on demand and displays recipient states", async () => {
        fetchMock.mockResolvedValue(historySuccess([
            { subject: "Test email", recipient: "me@example.com", state: "Pending", created_at: "2026-10-02T12:00:00Z", submitted_at: null },
            { subject: "Pub night", recipient: "old@example.com", state: "Delivered", created_at: "2026-10-01T12:00:00Z", submitted_at: "2026-10-01T12:01:00Z" },
            { subject: "Quiz night", recipient: "me@example.com", state: "Refused", created_at: "2026-09-30T12:00:00Z", submitted_at: null },
        ]));
        render(<EmailHistoryPanel />);
        expect(fetchMock).not.toHaveBeenCalled();
        fireEvent.click(screen.getByRole("button", { name: "Email Status" }));
        expect(screen.getByRole("status")).toHaveTextContent("Loading email history");
        await screen.findByText("Delivered");
        expect(screen.getByText("Pending")).toBeInTheDocument();
        expect(screen.getByText("Refused")).toBeInTheDocument();
        expect(screen.getByText("old@example.com")).toBeInTheDocument();
        expect(fetchMock).toHaveBeenCalledWith("http://localhost:8081/api/email-history", expect.objectContaining({
            headers: { Authorization: "Bearer firebase-id-token", "Content-Type": "application/json" },
            body: JSON.stringify({ request_id: requestID }),
        }));
    });

    it("shows empty history and refreshes only on demand", async () => {
        fetchMock.mockResolvedValue(historySuccess());
        render(<EmailHistoryPanel />);
        fireEvent.click(screen.getByRole("button", { name: "Email Status" }));
        await screen.findByText("No email history found.");
        fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
        await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
        await screen.findByText("No email history found.");
    });

    it.each([401, 503])("shows an error for HTTP %s", async (status) => {
        fetchMock.mockResolvedValue(jsonResponse({}, status));
        render(<EmailHistoryPanel />);
        fireEvent.click(screen.getByRole("button", { name: "Email Status" }));
        expect(await screen.findByRole("alert")).toBeInTheDocument();
        expect(fetchMock).toHaveBeenCalledTimes(1);
    });

    it("rejects another user's response", async () => {
        fetchMock.mockResolvedValue(jsonResponse({ status: "ok", uid: "another-user", request_id: requestID, entries: [] }));
        render(<EmailHistoryPanel />);
        fireEvent.click(screen.getByRole("button", { name: "Email Status" }));
        expect(await screen.findByRole("alert")).toHaveTextContent("unexpected response");
    });

    it("cancels on close without displaying a late result", async () => {
        let resolveResponse;
        fetchMock.mockReturnValue(new Promise((resolve) => { resolveResponse = resolve; }));
        render(<EmailHistoryPanel />);
        fireEvent.click(screen.getByRole("button", { name: "Email Status" }));
        await waitFor(() => expect(fetchMock).toHaveBeenCalledOnce());
        fireEvent.click(screen.getByText("Close", { selector: "button" }));
        expect(fetchMock.mock.calls[0][1].signal.aborted).toBe(true);
        await act(async () => { resolveResponse(historySuccess()); });
        expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    });

    it("closes and clears history on account change", async () => {
        fetchMock.mockResolvedValue(historySuccess());
        render(<EmailHistoryPanel />);
        fireEvent.click(screen.getByRole("button", { name: "Email Status" }));
        await screen.findByText("No email history found.");
        await act(async () => {
            authMock.currentUser = { uid: "another-user", getIdToken: getIdTokenMock };
            authListeners.forEach((listener) => listener(authMock.currentUser));
        });
        expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    });

    it("times out a stalled request", async () => {
        vi.useFakeTimers();
        fetchMock.mockReturnValue(new Promise(() => { }));
        render(<EmailHistoryPanel timeoutMs={100} />);
        fireEvent.click(screen.getByRole("button", { name: "Email Status" }));
        await act(async () => { await vi.advanceTimersByTimeAsync(100); });
        expect(screen.getByRole("alert")).toHaveTextContent("within the time limit");
    });
});

beforeEach(() => {
    vi.clearAllMocks();
    vi.stubEnv("VITE_LAST_ORDERS_API_BASE_URL", "");
    vi.stubGlobal("fetch", fetchMock);
    vi.spyOn(crypto, "randomUUID").mockReturnValue(requestID);
    authMock.currentUser = { uid: "signed-in-user", getIdToken: getIdTokenMock };
    getIdTokenMock.mockResolvedValue("firebase-id-token");
    fetchMock.mockResolvedValue(success());
});

afterEach(() => {
    cleanup();
    authListeners.clear();
    vi.useRealTimers();
    vi.unstubAllGlobals();
    vi.unstubAllEnvs();
    vi.restoreAllMocks();
});

describe("Last Orders API URL", () => {
    it("defaults to the separate local HTTP port", () => {
        expect(lastOrdersApiURL("api/ping")).toBe("http://localhost:8081/api/ping");
    });
    it("retains a configured HTTPS deployment prefix", () => {
        vi.stubEnv("VITE_LAST_ORDERS_API_BASE_URL", "https://backend.example/last-orders/");
        expect(lastOrdersApiURL("api/ping")).toBe("https://backend.example/last-orders/api/ping");
        expect(() => lastOrdersApiURL("../elsewhere")).toThrow();
        expect(() => lastOrdersApiURL("https://evil.example/api/ping")).toThrow();
    });
    it.each(["http://backend.example", "https://user:secret@backend.example", "https://backend.example?key=value"])("rejects insecure or credential-bearing config %s", (url) => {
        vi.stubEnv("VITE_LAST_ORDERS_API_BASE_URL", url);
        expect(() => lastOrdersApiURL("api/ping")).toThrow();
    });
});

describe("BackendPingPanel", () => {
    it("pings only on demand with a Firebase bearer token and displays verified identity", async () => {
        render(<BackendPingPanel />);
        expect(fetchMock).not.toHaveBeenCalled();
        fireEvent.click(screen.getByRole("button", { name: "Ping Last Orders" }));
        expect(screen.getByRole("button", { name: "Checking..." })).toBeDisabled();
        await screen.findByText("Authenticated");
        expect(screen.getByRole("status")).toHaveTextContent("Verified UID: signed-in-user");
        expect(fetchMock).toHaveBeenCalledTimes(1);
        expect(fetchMock).toHaveBeenCalledWith("http://localhost:8081/api/ping", expect.objectContaining({
            method: "POST",
            headers: { Authorization: "Bearer firebase-id-token", "Content-Type": "application/json" },
            body: JSON.stringify({ request_id: requestID }),
            credentials: "omit", cache: "no-store", redirect: "error",
        }));
    });

    it.each([[401, "Authentication Refused"], [403, "Access Refused"], [500, "Error"], [503, "Error"]])("handles HTTP %s without automatic retries", async (status, label) => {
        fetchMock.mockResolvedValue(jsonResponse({}, status));
        render(<BackendPingPanel />);
        fireEvent.click(screen.getByRole("button", { name: "Ping Last Orders" }));
        await screen.findByText(label);
        expect(fetchMock).toHaveBeenCalledTimes(1);
        expect(screen.getByRole("button", { name: "Ping Last Orders" })).toBeEnabled();
    });

    it("requires a Firebase user, not a UID supplied by the UI", async () => {
        authMock.currentUser = null;
        render(<BackendPingPanel />);
        fireEvent.click(screen.getByRole("button", { name: "Ping Last Orders" }));
        await screen.findByText(/Please sign in before contacting/);
        expect(fetchMock).not.toHaveBeenCalled();
    });

    it("reports token acquisition failure without sending a request or exposing details", async () => {
        getIdTokenMock.mockRejectedValue(new Error("secret token details"));
        render(<BackendPingPanel />);
        fireEvent.click(screen.getByRole("button", { name: "Ping Last Orders" }));
        await screen.findByText(/Unable to obtain a sign-in token/);
        expect(screen.getByRole("status")).not.toHaveTextContent("secret");
        expect(fetchMock).not.toHaveBeenCalled();
    });

    it("reports connection failures", async () => {
        fetchMock.mockRejectedValue(new TypeError("Failed to fetch"));
        render(<BackendPingPanel />);
        fireEvent.click(screen.getByRole("button", { name: "Ping Last Orders" }));
        await screen.findByText(/Unable to connect to Last Orders/);
    });

    it.each([
        { status: "ok", uid: "another-user", request_id: requestID },
        { status: "ok", uid: "signed-in-user", request_id: "wrong-id" },
        null,
    ])("rejects mismatched or malformed success payloads", async (data) => {
        fetchMock.mockResolvedValue(jsonResponse(data));
        render(<BackendPingPanel />);
        fireEvent.click(screen.getByRole("button", { name: "Ping Last Orders" }));
        await screen.findByText(/unexpected response/);
        expect(screen.queryByText("Authenticated")).not.toBeInTheDocument();
    });

    it("rejects non-JSON and invalid JSON responses", async () => {
        const response = success();
        response.headers.set("Content-Type", "text/html");
        fetchMock.mockResolvedValueOnce(response).mockResolvedValueOnce({ ...success(), json: () => Promise.reject(new Error("bad JSON")) });
        await expect(requestLastOrders("api/ping")).rejects.toMatchObject({ code: "invalid_response" });
        await expect(requestLastOrders("api/ping")).rejects.toMatchObject({ code: "invalid_response" });
    });

    it("prevents duplicate clicks and times out token acquisition before a late token can send", async () => {
        vi.useFakeTimers();
        let resolveToken;
        getIdTokenMock.mockReturnValue(new Promise((resolve) => { resolveToken = resolve; }));
        render(<BackendPingPanel timeoutMs={100} />);
        const button = screen.getByRole("button", { name: "Ping Last Orders" });
        fireEvent.click(button);
        fireEvent.click(button);
        expect(getIdTokenMock).toHaveBeenCalledTimes(1);
        await act(async () => { await vi.advanceTimersByTimeAsync(100); });
        expect(screen.getByText("Timeout")).toBeInTheDocument();
        await act(async () => { resolveToken("late-token"); });
        expect(fetchMock).not.toHaveBeenCalled();
    });

    it("aborts a stalled HTTP request when the deadline expires", async () => {
        vi.useFakeTimers();
        fetchMock.mockReturnValue(new Promise(() => { }));
        render(<BackendPingPanel timeoutMs={100} />);
        fireEvent.click(screen.getByRole("button", { name: "Ping Last Orders" }));
        await act(async () => { await vi.advanceTimersByTimeAsync(100); });
        expect(screen.getByText("Timeout")).toBeInTheDocument();
        expect(fetchMock.mock.calls[0][1].signal.aborted).toBe(true);
    });

    it("cancels on sign-out and ignores a late response", async () => {
        let resolveResponse;
        fetchMock.mockReturnValue(new Promise((resolve) => { resolveResponse = resolve; }));
        render(<BackendPingPanel />);
        fireEvent.click(screen.getByRole("button", { name: "Ping Last Orders" }));
        await waitFor(() => expect(fetchMock).toHaveBeenCalledOnce());
        await act(async () => {
            authMock.currentUser = null;
            authListeners.forEach((listener) => listener(null));
        });
        expect(screen.getByText("Not Checked")).toBeInTheDocument();
        await act(async () => { resolveResponse(success()); });
        expect(screen.queryByText("Authenticated")).not.toBeInTheDocument();
        expect(fetchMock.mock.calls[0][1].signal.aborted).toBe(true);
    });

    it("aborts on unmount and unsubscribes from authentication", async () => {
        fetchMock.mockReturnValue(new Promise(() => { }));
        const view = render(<BackendPingPanel />);
        fireEvent.click(screen.getByRole("button", { name: "Ping Last Orders" }));
        await waitFor(() => expect(fetchMock).toHaveBeenCalledOnce());
        view.unmount();
        expect(fetchMock.mock.calls[0][1].signal.aborted).toBe(true);
        expect(authListeners.size).toBe(0);
    });
});
