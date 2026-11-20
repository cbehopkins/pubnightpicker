import { afterEach, describe, expect, it, vi } from "vitest";
import { streamLastOrders } from "./lastOrdersApi";

const { authMock, fetchEventSourceMock } = vi.hoisted(() => ({
  authMock: { currentUser: null },
  fetchEventSourceMock: vi.fn(),
}));

vi.mock("../firebase", () => ({ auth: authMock }));
vi.mock("@microsoft/fetch-event-source", () => ({ fetchEventSource: fetchEventSourceMock }));

afterEach(() => {
  authMock.currentUser = null;
  fetchEventSourceMock.mockReset();
  vi.unstubAllGlobals();
});

describe("streamLastOrders", () => {
  it("refreshes the bearer token for reconnects and preserves the SSE cursor", async () => {
    let capturedOptions;
    fetchEventSourceMock.mockImplementation((_url, options) => {
      capturedOptions = options;
    });
    const user = {
      uid: "user-1",
      getIdToken: vi.fn()
        .mockResolvedValueOnce("token-1")
        .mockResolvedValueOnce("token-2"),
    };
    authMock.currentUser = user;
    const fetchMock = vi.fn(async () => new Response("", { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    await streamLastOrders("api/meets/group-a/open/stream", { signal: new AbortController().signal });
    await capturedOptions.fetch("http://localhost:8081/api/meets/group-a/open/stream", {
      headers: new Headers({ "Last-Event-ID": "cursor-1" }),
    });
    await capturedOptions.fetch("http://localhost:8081/api/meets/group-a/open/stream", {
      headers: new Headers({ "Last-Event-ID": "cursor-2" }),
    });

    expect(fetchEventSourceMock).toHaveBeenCalledWith(
      expect.stringContaining("/api/meets/group-a/open/stream"),
      expect.objectContaining({ openWhenHidden: true }),
    );
    expect(user.getIdToken).toHaveBeenCalledTimes(2);
    expect(new Headers(fetchMock.mock.calls[0][1].headers).get("Authorization")).toBe("Bearer token-1");
    expect(new Headers(fetchMock.mock.calls[0][1].headers).get("Last-Event-ID")).toBe("cursor-1");
    expect(new Headers(fetchMock.mock.calls[1][1].headers).get("Authorization")).toBe("Bearer token-2");
    expect(new Headers(fetchMock.mock.calls[1][1].headers).get("Last-Event-ID")).toBe("cursor-2");
  });

  it("does not send a token after the authenticated session changes", async () => {
    let capturedOptions;
    fetchEventSourceMock.mockImplementation((_url, options) => {
      capturedOptions = options;
    });
    authMock.currentUser = {
      uid: "user-1",
      getIdToken: async () => {
        authMock.currentUser = { uid: "user-2", getIdToken: vi.fn() };
        return "stale-token";
      },
    };
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    await streamLastOrders("api/meets/group-a/open/stream", { signal: new AbortController().signal });

    await expect(capturedOptions.fetch("http://localhost:8081/api/meets/group-a/open/stream", {}))
      .rejects.toMatchObject({ code: "signed_out" });
    expect(fetchMock).not.toHaveBeenCalled();
  });
});