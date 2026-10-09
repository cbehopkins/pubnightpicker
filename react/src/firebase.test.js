// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from "vitest";

const {
    authState,
    verifyBeforeUpdateEmailMock,
    reauthenticateWithCredentialMock,
    credentialMock,
} = vi.hoisted(() => {
    return {
        authState: {
            currentUser: null,
        },
        verifyBeforeUpdateEmailMock: vi.fn(async () => undefined),
        reauthenticateWithCredentialMock: vi.fn(async () => undefined),
        credentialMock: vi.fn((email, password) => ({ email, password })),
    };
});

vi.mock("firebase/app", () => {
    return {
        initializeApp: vi.fn(() => ({ app: true })),
    };
});

vi.mock("firebase/auth", () => {
    function GoogleAuthProviderMock() { }

    return {
        connectAuthEmulator: vi.fn(),
        GoogleAuthProvider: GoogleAuthProviderMock,
        EmailAuthProvider: {
            credential: credentialMock,
        },
        getAuth: vi.fn(() => authState),
        signInWithPopup: vi.fn(),
        signInWithEmailAndPassword: vi.fn(),
        fetchSignInMethodsForEmail: vi.fn(),
        createUserWithEmailAndPassword: vi.fn(),
        reauthenticateWithCredential: reauthenticateWithCredentialMock,
        sendPasswordResetEmail: vi.fn(),
        signOut: vi.fn(),
        verifyBeforeUpdateEmail: verifyBeforeUpdateEmailMock,
    };
});

vi.mock("firebase/firestore", () => {
    return {
        connectFirestoreEmulator: vi.fn(),
        initializeFirestore: vi.fn(() => ({ db: true })),
        persistentLocalCache: vi.fn(() => ({})),
        persistentMultipleTabManager: vi.fn(() => ({})),
        doc: vi.fn(() => ({})),
        getDoc: vi.fn(),
        setDoc: vi.fn(),
        query: vi.fn(),
        getDocs: vi.fn(),
        collection: vi.fn(),
        where: vi.fn(),
        addDoc: vi.fn(),
    };
});

vi.mock("react-router-dom", () => {
    return {
        redirect: vi.fn((path) => ({ path })),
    };
});

vi.mock("./utils/notify", () => {
    return {
        notifyError: vi.fn(),
        notifyInfo: vi.fn(),
    };
});

import { reauthenticatePasswordUser, requestLoginEmailChange } from "./firebase";
import { db, logInWithEmailAndPassword, signInWithGoogle } from "./firebase";
import { signInWithEmailAndPassword, signInWithPopup } from "firebase/auth";
import { doc, getDoc, getDocs, setDoc } from "firebase/firestore";
import { notifyError } from "./utils/notify";

describe("login profile initialization", () => {
    const user = { uid: "new-user", displayName: "New User", email: "new@example.com" };
    beforeEach(() => {
        vi.clearAllMocks();
        vi.mocked(signInWithPopup).mockResolvedValue({ user });
        vi.mocked(signInWithEmailAndPassword).mockResolvedValue({ user });
        vi.mocked(doc).mockImplementation((_db, collectionName, uid) => ({ path: `${collectionName}/${uid}` }));
        vi.mocked(setDoc).mockResolvedValue(undefined);
    });

    for (const provider of ["google", "local"]) {
        const login = () => provider === "google" ? signInWithGoogle() : logInWithEmailAndPassword(user.email, "password");

        it(`initializes missing profiles on ${provider} login using a direct UID document read`, async () => {
            vi.mocked(getDoc).mockResolvedValue({ exists: () => false });
            await login();
            expect(doc).toHaveBeenCalledWith(db, "users", user.uid);
            expect(getDoc).toHaveBeenCalledWith({ path: "users/new-user" });
            expect(getDocs).not.toHaveBeenCalled();
            expect(setDoc).toHaveBeenCalledWith({ path: "users/new-user" }, {
                uid: user.uid, name: user.displayName, email: user.email, authProvider: provider,
            }, { merge: true });
            expect(setDoc).toHaveBeenCalledWith({ path: "user-public/new-user" }, {
                uid: user.uid, name: user.displayName, photoUrl: null, votesVisible: true,
            }, { merge: true });
            expect(notifyError).not.toHaveBeenCalled();
        });

        it(`preserves existing profiles on ${provider} login`, async () => {
            vi.mocked(getDoc).mockResolvedValue({ exists: () => true });
            await login();
            expect(getDoc).toHaveBeenCalledWith({ path: "users/new-user" });
            expect(getDocs).not.toHaveBeenCalled();
            expect(setDoc).not.toHaveBeenCalled();
            expect(notifyError).not.toHaveBeenCalled();
        });
    }

    it("reports a Google profile read failure without attempting writes", async () => {
        const error = new Error("Profile read denied");
        vi.mocked(getDoc).mockRejectedValue(error);
        const consoleError = vi.spyOn(console, "error").mockImplementation(() => { });
        try {
            await signInWithGoogle();
            expect(notifyError).toHaveBeenCalledWith(error.message);
            expect(setDoc).not.toHaveBeenCalled();
        } finally {
            consoleError.mockRestore();
        }
    });
});

describe("firebase auth helpers", () => {
    beforeEach(() => {
        authState.currentUser = null;
        verifyBeforeUpdateEmailMock.mockReset();
        reauthenticateWithCredentialMock.mockReset();
        credentialMock.mockReset();
        credentialMock.mockImplementation((email, password) => ({ email, password }));
    });

    it("returns no-current-user for email change when there is no active user", async () => {
        const result = await requestLoginEmailChange("new@example.com");

        expect(result.ok).toBe(false);
        expect(result.code).toBe("auth/no-current-user");
    });

    it("normalizes email and requests verify-before-update", async () => {
        authState.currentUser = { uid: "user-1", email: "old@example.com" };

        const result = await requestLoginEmailChange(" New.Email@Example.COM ");

        expect(verifyBeforeUpdateEmailMock).toHaveBeenCalledWith(
            authState.currentUser,
            "new.email@example.com"
        );
        expect(result).toEqual({
            ok: true,
            email: "new.email@example.com",
        });
    });

    it("returns requiresRecentLogin response for stale session", async () => {
        authState.currentUser = { uid: "user-1", email: "old@example.com" };
        verifyBeforeUpdateEmailMock.mockRejectedValueOnce({
            code: "auth/requires-recent-login",
            message: "Need recent login",
        });

        const result = await requestLoginEmailChange("new@example.com");

        expect(result.ok).toBe(false);
        expect(result.requiresRecentLogin).toBe(true);
        expect(result.email).toBe("new@example.com");
        expect(result.message).toBe("Please re-enter your password to continue");
    });

    it("maps email already in use errors for email change", async () => {
        authState.currentUser = { uid: "user-1", email: "old@example.com" };
        verifyBeforeUpdateEmailMock.mockRejectedValueOnce({
            code: "auth/email-already-in-use",
            message: "Email exists",
        });

        const result = await requestLoginEmailChange("new@example.com");

        expect(result.ok).toBe(false);
        expect(result.message).toBe("Another account already uses that email address");
    });

    it("validates blank password before reauthentication", async () => {
        authState.currentUser = { uid: "user-1", email: "person@example.com" };

        const result = await reauthenticatePasswordUser("");

        expect(result.ok).toBe(false);
        expect(result.code).toBe("validation/blank-password");
        expect(credentialMock).not.toHaveBeenCalled();
    });

    it("reauthenticates with normalized current email", async () => {
        authState.currentUser = { uid: "user-1", email: " Person@Example.com " };

        const result = await reauthenticatePasswordUser("secret123");

        expect(credentialMock).toHaveBeenCalledWith("person@example.com", "secret123");
        expect(reauthenticateWithCredentialMock).toHaveBeenCalledWith(authState.currentUser, {
            email: "person@example.com",
            password: "secret123",
        });
        expect(result).toEqual({ ok: true });
    });

    it("maps wrong password errors during reauthentication", async () => {
        authState.currentUser = { uid: "user-1", email: "person@example.com" };
        reauthenticateWithCredentialMock.mockRejectedValueOnce({
            code: "auth/wrong-password",
            message: "Wrong password",
        });

        const result = await reauthenticatePasswordUser("bad-pass");

        expect(result.ok).toBe(false);
        expect(result.message).toBe("Incorrect password");
    });
});
