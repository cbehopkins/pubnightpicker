// @vitest-environment jsdom

import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
    auth: { currentUser: { uid: "viewer" } },
    db: {},
    dispatch: vi.fn(),
    onSnapshot: vi.fn(),
    snapshotCallback: null,
}));

vi.mock("../firebase", () => ({ auth: mocks.auth, db: mocks.db }));
vi.mock("react-firebase-hooks/auth", () => ({
    useAuthState: () => [mocks.auth.currentUser, false],
}));
vi.mock("react-redux", () => ({ useDispatch: () => mocks.dispatch }));
vi.mock("firebase/firestore", () => ({
    collection: vi.fn((_db, name) => ({ name })),
    onSnapshot: mocks.onSnapshot,
}));

import { useUsersSource } from "./useUsers";
import { userAdded } from "../store/usersSlice";

describe("useUsersSource public avatar selection", () => {
    beforeEach(() => {
        vi.clearAllMocks();
        mocks.snapshotCallback = null;
        mocks.onSnapshot.mockImplementation((_query, callback) => {
            mocks.snapshotCallback = callback;
            return vi.fn();
        });
    });

    it("prefers customPhotoUrl and falls back to the provider photoUrl", () => {
        renderHook(() => useUsersSource());

        act(() => {
            mocks.snapshotCallback({
                docChanges: () => [
                    {
                        type: "added",
                        doc: {
                            id: "user-1",
                            data: () => ({
                                uid: "user-1",
                                name: "User One",
                                photoUrl: "https://provider.example/avatar.png",
                                customPhotoUrl: "https://custom.example/avatar.png",
                                votesVisible: true,
                            }),
                        },
                    },
                    {
                        type: "added",
                        doc: {
                            id: "user-2",
                            data: () => ({
                                uid: "user-2",
                                name: "User Two",
                                photoUrl: "https://provider.example/avatar.png",
                                customPhotoUrl: "",
                                votesVisible: true,
                            }),
                        },
                    },
                ],
            });
        });

        expect(mocks.dispatch).toHaveBeenNthCalledWith(
            1,
            userAdded({
                uid: "user-1",
                name: "User One",
                photoUrl: "https://custom.example/avatar.png",
                votesVisible: true,
            }),
        );
        expect(mocks.dispatch).toHaveBeenNthCalledWith(
            2,
            userAdded({
                uid: "user-2",
                name: "User Two",
                photoUrl: "https://provider.example/avatar.png",
                votesVisible: true,
            }),
        );
    });
});
