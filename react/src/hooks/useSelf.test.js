// @vitest-environment jsdom

import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
    auth: { currentUser: null },
    db: {},
    dispatch: vi.fn(),
    doc: vi.fn((_db, collection, uid) => ({ collection, uid })),
    listeners: {},
    onSnapshot: vi.fn(),
    setDoc: vi.fn(async () => undefined),
}));

vi.mock("../firebase", () => ({ auth: mocks.auth, db: mocks.db }));
vi.mock("react-firebase-hooks/auth", () => ({
    useAuthState: () => [mocks.auth.currentUser, false],
}));
vi.mock("react-redux", () => ({ useDispatch: () => mocks.dispatch }));
vi.mock("firebase/firestore", () => ({
    doc: mocks.doc,
    onSnapshot: mocks.onSnapshot,
    setDoc: mocks.setDoc,
}));
vi.mock("../utils/notify", () => ({ notifyError: vi.fn() }));

import useSelf from "./useSelf";
import { authAdded } from "../store/authSlice";

function snapshot(data) {
    return { exists: () => true, data: () => data };
}

describe("useSelf profile hydration", () => {
    beforeEach(() => {
        vi.clearAllMocks();
        mocks.listeners = {};
        mocks.auth.currentUser = {
            uid: "user-1",
            displayName: "Auth Name",
            email: "user@example.com",
            photoURL: "https://provider.example/avatar.png",
            providerData: [{ providerId: "google.com" }],
        };
        mocks.setDoc.mockResolvedValue(undefined);
        mocks.onSnapshot.mockImplementation((reference, callback) => {
            mocks.listeners[reference.collection] = callback;
            return vi.fn();
        });
    });

    it("uses public profile fields and refreshes only the public Google avatar", async () => {
        renderHook(() => useSelf());

        act(() => {
            mocks.listeners.users(snapshot({
                uid: "user-1",
                name: "Legacy private name",
                photoUrl: "https://private.example/old.png",
                votesVisible: false,
                authProvider: "google",
                customPhotoUrl: false,
                email: "user@example.com",
            }));
            mocks.listeners["user-public"](snapshot({
                uid: "user-1",
                name: "Public preferred name",
                photoUrl: "https://provider.example/old.png",
                votesVisible: true,
            }));
        });

        await waitFor(() => {
            expect(mocks.dispatch).toHaveBeenLastCalledWith(authAdded({
                uid: "user-1",
                name: "Public preferred name",
                email: "user@example.com",
                photoUrl: "https://provider.example/avatar.png",
            }));
        });

        expect(mocks.setDoc).toHaveBeenCalledWith(
            { collection: "user-public", uid: "user-1" },
            { uid: "user-1", photoUrl: "https://provider.example/avatar.png" },
            { merge: true },
        );
        expect(mocks.setDoc).not.toHaveBeenCalledWith(
            { collection: "users", uid: "user-1" },
            expect.objectContaining({ name: expect.anything(), photoUrl: expect.anything() }),
            expect.anything(),
        );
    });
});
