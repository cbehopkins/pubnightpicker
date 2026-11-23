// @vitest-environment jsdom

import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import useVisibleVoterProfiles from "./useVisibleVoterProfiles";

const { docMock, getDocMock } = vi.hoisted(() => ({
  docMock: vi.fn((_db, collection, uid) => ({ collection, uid })),
  getDocMock: vi.fn(),
}));

vi.mock("../firebase", () => ({ db: {} }));
vi.mock("firebase/firestore", () => ({ doc: docMock, getDoc: getDocMock }));

describe("useVisibleVoterProfiles", () => {
  beforeEach(() => {
    docMock.mockClear();
    getDocMock.mockReset();
    getDocMock.mockImplementation(async (reference) => ({
      exists: () => true,
      id: reference.uid,
      data: () => ({ name: `Name ${reference.uid}`, votesVisible: true }),
    }));
  });

  it("resolves only the unique UIDs supplied by the privacy-safe vote snapshot", async () => {
    const { result } = renderHook(() => useVisibleVoterProfiles(["visible-1", "visible-1", "visible-2"]));

    await waitFor(() => expect(Object.keys(result.current)).toHaveLength(2));

    expect(docMock.mock.calls.map(([, collection, uid]) => [collection, uid])).toEqual([
      ["user-public", "visible-1"],
      ["user-public", "visible-2"],
    ]);
    expect(result.current["visible-1"]).toEqual({ uid: "visible-1", name: "Name visible-1", votesVisible: true });
  });
});