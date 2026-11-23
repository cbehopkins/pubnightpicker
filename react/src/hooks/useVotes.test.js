// @vitest-environment jsdom

import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import useVotes from "./useVotes";

const { requestLastOrdersMock, streamLastOrdersMock } = vi.hoisted(() => ({
  requestLastOrdersMock: vi.fn(),
  streamLastOrdersMock: vi.fn(),
}));

vi.mock("../utils/lastOrdersApi", () => ({
  requestLastOrders: requestLastOrdersMock,
  streamLastOrders: streamLastOrdersMock,
}));

vi.mock("../firebase", () => ({ db: {} }));
vi.mock("firebase/firestore", () => ({
  arrayRemove: vi.fn(),
  arrayUnion: vi.fn(),
  doc: vi.fn(() => ({})),
  onSnapshot: vi.fn(() => () => undefined),
  runTransaction: vi.fn(),
}));

const initialSnapshot = {
  options: [
    { id: "venue-1", total: 3, anonymous_count: 1, voters: [{ id: "visible-user", name: "Alice", photo_url: "https://example.test/alice.jpg" }, { id: "current-user" }] },
    { id: "any", total: 1, anonymous_count: 0, voters: [{ id: "current-user" }] },
  ],
  selected_option_ids: ["any", "removed-venue"],
};

let streamOptions;

describe("useVotes", () => {
  beforeEach(() => {
    requestLastOrdersMock.mockReset();
    streamLastOrdersMock.mockReset();
    streamOptions = null;
    requestLastOrdersMock.mockResolvedValue(initialSnapshot);
    streamLastOrdersMock.mockImplementation((_path, options) => {
      streamOptions = options;
      return new Promise((resolve) => options.signal.addEventListener("abort", resolve, { once: true }));
    });
  });

  it("loads privacy-safe vote state and follows complete stream snapshots", async () => {
    const { result } = renderHook(() => useVotes("poll-1", "current-user"));

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    await waitFor(() => expect(streamOptions).not.toBeNull());

    expect(requestLastOrdersMock).toHaveBeenCalledWith("api/meet/poll-1/votes", expect.objectContaining({ method: "GET" }));
    expect(result.current.optionsById).toEqual({
      "venue-1": { total: 3, anonymousCount: 1, voterUids: ["visible-user", "current-user"], voters: [
        { uid: "visible-user", name: "Alice", photoUrl: "https://example.test/alice.jpg" },
        { uid: "current-user", name: undefined, photoUrl: undefined },
      ] },
      any: { total: 1, anonymousCount: 0, voterUids: ["current-user"], voters: [{ uid: "current-user", name: undefined, photoUrl: undefined }] },
    });
    expect(result.current.voterProfiles["visible-user"]).toEqual({ uid: "visible-user", name: "Alice", photoUrl: "https://example.test/alice.jpg" });
    expect(result.current.selectedOptionIds).toEqual(["any", "removed-venue"]);
    expect(requestLastOrdersMock).toHaveBeenCalledTimes(1);

    act(() => streamOptions.onmessage({
      event: "voting_snapshot",
      data: JSON.stringify({
        cursor: "cursor-2",
        options: [{ id: "venue-1", total: 2, anonymous_count: 1, voters: [{ id: "visible-user", name: "Updated Alice" }] }],
        selected_option_ids: ["removed-venue", "venue-1"],
      }),
    }));

    await waitFor(() => expect(result.current.selectedOptionIds).toEqual(["removed-venue", "venue-1"]));
    expect(requestLastOrdersMock).toHaveBeenCalledTimes(1);
    expect(result.current.optionsById["venue-1"]).toEqual({ total: 2, anonymousCount: 1, voterUids: ["visible-user"], voters: [{ uid: "visible-user", name: "Updated Alice", photoUrl: undefined }] });
    expect(result.current.voterProfiles["visible-user"].name).toBe("Updated Alice");
    expect(result.current.voterProfiles["current-user"]).toBeUndefined();
  });

  it("retains a removed vote when its candidate returns", async () => {
    const { result } = renderHook(() => useVotes("poll-1", "current-user"));
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    act(() => streamOptions.onmessage({
      event: "voting_snapshot",
      data: JSON.stringify({
        cursor: "cursor-2",
        options: [{ id: "removed-venue", total: 1, anonymous_count: 0, voters: [] }],
        selected_option_ids: ["removed-venue"],
      }),
    }));

    expect(result.current.selectedOptionIds).toEqual(["removed-venue"]);
    expect(result.current.optionsById["removed-venue"].total).toBe(1);
    expect(requestLastOrdersMock).toHaveBeenCalledTimes(1);
  });

  it("withdraws a removed candidate only on request", async () => {
    const { result } = renderHook(() => useVotes("poll-1", "current-user"));
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.selectedOptionIds).toContain("removed-venue");
    expect(requestLastOrdersMock).toHaveBeenCalledTimes(1);

    await act(async () => {
      await result.current.changeVote("removed-venue", false);
    });
    expect(requestLastOrdersMock).toHaveBeenLastCalledWith("api/meet/poll-1/votes/removed-venue", { method: "DELETE" });
    expect(result.current.selectedOptionIds).not.toContain("removed-venue");
  });

  it("does not connect when disabled", () => {
    const { result } = renderHook(() => useVotes("poll-1", false));

    expect(requestLastOrdersMock).not.toHaveBeenCalled();
    expect(streamLastOrdersMock).not.toHaveBeenCalled();
    expect(result.current.optionsById).toEqual({});
  });

  it("adds and removes votes through the authenticated API", async () => {
    const { result } = renderHook(() => useVotes("poll-1", "current-user"));
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    await act(async () => {
      await result.current.changeVote("venue-1", true);
    });

    expect(requestLastOrdersMock).toHaveBeenLastCalledWith("api/meet/poll-1/votes/venue-1", { method: "PUT" });

    await act(async () => {
      await result.current.changeVote("any", false);
    });

    expect(requestLastOrdersMock).toHaveBeenLastCalledWith("api/meet/poll-1/votes/any", { method: "DELETE" });
  });

  it("keeps an uncertain latest selection available for an explicit retry", async () => {
    const emptySnapshot = {
      options: [{ id: "venue-1", total: 0, anonymous_count: 0, voters: [] }],
      selected_option_ids: [],
    };
    requestLastOrdersMock
      .mockResolvedValueOnce(emptySnapshot)
      .mockRejectedValueOnce(new Error("request timed out"))
      .mockResolvedValueOnce(emptySnapshot)
      .mockResolvedValueOnce(undefined);
    const { result } = renderHook(() => useVotes("poll-1", "current-user"));
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    await act(async () => {
      expect(await result.current.changeVote("venue-1", true)).toBe(false);
    });

    expect(result.current.retrySelections).toEqual({ "venue-1": true });
    expect(result.current.selectedOptionIds).toEqual([]);

    await act(async () => {
      expect(await result.current.retryVote("venue-1")).toBe(true);
    });

    expect(requestLastOrdersMock).toHaveBeenLastCalledWith("api/meet/poll-1/votes/venue-1", { method: "PUT" });
    expect(result.current.retrySelections).toEqual({});
  });

  it("retries even when an unavailable reconciliation leaves the optimistic selection in place", async () => {
    const emptySnapshot = {
      options: [{ id: "venue-1", total: 0, anonymous_count: 0, voters: [] }],
      selected_option_ids: [],
    };
    requestLastOrdersMock
      .mockResolvedValueOnce(emptySnapshot)
      .mockRejectedValueOnce(new Error("request timed out"))
      .mockRejectedValueOnce(new Error("snapshot unavailable"))
      .mockResolvedValueOnce(undefined);
    const { result } = renderHook(() => useVotes("poll-1", "current-user"));
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    await act(async () => {
      expect(await result.current.changeVote("venue-1", true)).toBe(false);
    });
    expect(result.current.selectedOptionIds).toEqual(["venue-1"]);
    expect(result.current.retrySelections).toEqual({ "venue-1": true });

    await act(async () => {
      expect(await result.current.retryVote("venue-1")).toBe(true);
    });

    expect(requestLastOrdersMock).toHaveBeenLastCalledWith("api/meet/poll-1/votes/venue-1", { method: "PUT" });
  });
});
