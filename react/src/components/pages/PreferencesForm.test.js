// @vitest-environment jsdom

import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, fireEvent, waitFor } from "@testing-library/react";

const useWebPushSettingsMock = vi.hoisted(() => vi.fn());
const getDocMock = vi.hoisted(() => vi.fn());
const firestoreDocMock = vi.hoisted(() => vi.fn());
const setDocMock = vi.hoisted(() => vi.fn(async () => undefined));
const updateDocMock = vi.hoisted(() => vi.fn(async () => undefined));

vi.mock("firebase/firestore", () => ({
    doc: firestoreDocMock,
    getDoc: getDocMock,
    setDoc: setDocMock,
    updateDoc: updateDocMock,
}));

vi.mock("../../firebase", () => ({
    db: {},
}));

vi.mock("react-redux", () => ({
    useSelector: (selector) => selector({
        auth: {
            loggedIn: true,
            photoUrl: "",
            uid: "user-1",
        },
    }),
}));

vi.mock("react-router-dom", async (importOriginal) => {
    const original = await importOriginal();
    return {
        ...original,
        useNavigate: () => vi.fn(),
        useNavigation: () => ({ state: "idle" }),
    };
});

vi.mock("../../hooks/useWebPushSettings", () => ({
    default: useWebPushSettingsMock,
}));

import PreferencesForm, { PushPreferences } from "./PreferencesForm";

afterEach(() => {
    cleanup();
    vi.clearAllMocks();
});

describe("PreferencesForm", () => {
    it("restores all saved fields after browser state restoration", async () => {
        let resolvePrivateProfile;
        const privateProfile = new Promise((resolve) => {
            resolvePrivateProfile = resolve;
        });
        let resolvePublicProfile;
        const publicProfile = new Promise((resolve) => {
            resolvePublicProfile = resolve;
        });

        firestoreDocMock.mockImplementation((_db, collection, uid) => ({ collection, uid }));
        getDocMock.mockImplementation(({ collection }) => collection === "users"
            ? privateProfile
            : publicProfile);
        useWebPushSettingsMock.mockReturnValue({ featureEnabled: false });

        render(<PreferencesForm method="post" uid="managed-user" isAdminEditing />);

        const nameInput = screen.getByLabelText("My Preferred Name");
        const avatarInput = screen.getByLabelText("Chat Avatar");
        const emailEnabledCheckbox = screen.getByLabelText("Email Me");
        const emailInput = screen.getByLabelText("Email Address");
        const votesVisibleCheckbox = screen.getByLabelText("Votes Visible to Known Users");
        const openPollEmailCheckbox = screen.getByLabelText("Email me when a poll opens");
        const arrivalTimeInput = screen.getByLabelText("Default arrival time (ETA)");

        nameInput.value = "Restored stale name";
        avatarInput.value = "https://stale.example/avatar.png";
        emailEnabledCheckbox.checked = false;
        emailInput.value = "stale@example.com";
        votesVisibleCheckbox.checked = true;
        openPollEmailCheckbox.checked = false;
        arrivalTimeInput.value = "19:30";

        await act(async () => {
            resolvePrivateProfile({
                exists: () => true,
                data: () => ({
                    notificationEmail: "alerts@example.com",
                    notificationEmailEnabled: true,
                    openPollEmailEnabled: true,
                    defaultArrivalTime: "18:45",
                }),
            });
            resolvePublicProfile({
                exists: () => true,
                data: () => ({
                    name: "Managed User",
                    photoUrl: "https://example.com/avatar.png",
                    votesVisible: false,
                }),
            });
            await Promise.all([privateProfile, publicProfile]);
        });

        expect(nameInput.value).toBe("Managed User");
        expect(avatarInput.value).toBe("https://example.com/avatar.png");
        expect(emailEnabledCheckbox.checked).toBe(true);
        expect(emailInput.value).toBe("alerts@example.com");
        expect(votesVisibleCheckbox.checked).toBe(false);
        expect(openPollEmailCheckbox.checked).toBe(true);
        expect(arrivalTimeInput.value).toBe("18:45");
        expect(firestoreDocMock).toHaveBeenCalledWith({}, "users", "managed-user");
        expect(firestoreDocMock).toHaveBeenCalledWith({}, "user-public", "managed-user");
    });

    it("saves edited private and public preferences for the managed user", async () => {
        firestoreDocMock.mockImplementation((_db, collection, uid) => ({ collection, uid }));
        getDocMock.mockImplementation(({ collection }) => Promise.resolve({
            exists: () => true,
            data: () => collection === "users"
                ? {
                    notificationEmail: "old@example.com",
                    notificationEmailEnabled: true,
                    openPollEmailEnabled: true,
                    defaultArrivalTime: "18:45",
                    webPushEnabled: true,
                    pushPreferences: {
                        pollOpens: true,
                        pollCompletes: false,
                        globalChat: false,
                        eventChat: true,
                    },
                }
                : {
                    name: "Managed User",
                    photoUrl: "https://example.com/old-avatar.png",
                    votesVisible: false,
                },
        }));
        const onCancel = vi.fn();

        render(
            <PreferencesForm
                method="post"
                uid="managed-user"
                isAdminEditing
                onCancel={onCancel}
            />
        );

        await waitFor(() => {
            expect(screen.getByLabelText("My Preferred Name").value).toBe("Managed User");
        });

        fireEvent.change(screen.getByLabelText("My Preferred Name"), { target: { value: "Updated User" } });
        fireEvent.change(screen.getByLabelText("Chat Avatar"), { target: { value: "https://example.com/new-avatar.png" } });
        fireEvent.click(screen.getByLabelText("Email Me"));
        fireEvent.change(screen.getByLabelText("Email Address"), { target: { value: "new@example.com" } });
        fireEvent.click(screen.getByLabelText("Votes Visible to Known Users"));
        fireEvent.click(screen.getByLabelText("Email me when a poll opens"));
        fireEvent.change(screen.getByLabelText("Default arrival time (ETA)"), { target: { value: "20:15" } });
        fireEvent.click(screen.getByLabelText("A message is sent in global chat"));
        fireEvent.click(screen.getByRole("button", { name: "Save" }));

        await waitFor(() => expect(onCancel).toHaveBeenCalledTimes(1));
        expect(updateDocMock).toHaveBeenCalledWith(
            { collection: "users", uid: "managed-user" },
            expect.objectContaining({
                notificationEmail: "new@example.com",
                notificationEmailEnabled: false,
                openPollEmailEnabled: false,
                defaultArrivalTime: "20:15",
                pushPreferences: {
                    pollOpens: true,
                    pollCompletes: false,
                    globalChat: true,
                    eventChat: true,
                },
            })
        );
        expect(setDocMock).toHaveBeenCalledWith(
            { collection: "user-public", uid: "managed-user" },
            expect.objectContaining({
                uid: "managed-user",
                name: "Updated User",
                photoUrl: "https://example.com/new-avatar.png",
                votesVisible: true,
            }),
            { merge: true }
        );
    });

    it("closes admin editing without saving when cancelled", () => {
        firestoreDocMock.mockImplementation((_db, collection, uid) => ({ collection, uid }));
        getDocMock.mockResolvedValue({ exists: () => false });
        const onCancel = vi.fn();

        render(
            <PreferencesForm
                method="post"
                uid="managed-user"
                isAdminEditing
                onCancel={onCancel}
            />
        );

        fireEvent.click(screen.getByRole("button", { name: "Cancel" }));

        expect(onCancel).toHaveBeenCalledTimes(1);
        expect(updateDocMock).not.toHaveBeenCalled();
        expect(setDocMock).not.toHaveBeenCalled();
    });
});

describe("PushPreferences", () => {
    it("updates checkbox state when pushPreferences are loaded after initial render", () => {
        useWebPushSettingsMock.mockReturnValue({
            busy: false,
            disable: vi.fn(),
            enable: vi.fn(),
            enabled: true,
            error: "",
            featureEnabled: true,
            permission: "granted",
            supported: true,
        });

        const { rerender } = render(
            <PushPreferences
                uid="user-1"
                initialEnabled={true}
                pushPreferences={null}
            />
        );

        const globalChatCheckbox = screen.getByLabelText("A message is sent in global chat");
        expect(globalChatCheckbox.checked).toBe(false);

        rerender(
            <PushPreferences
                uid="user-1"
                initialEnabled={true}
                pushPreferences={{
                    pollOpens: true,
                    pollCompletes: true,
                    globalChat: true,
                    eventChat: false,
                }}
            />
        );

        expect(screen.getByLabelText("A message is sent in global chat").checked).toBe(true);
    });

    it("shows migration defaults when pushPreferences are missing", () => {
        useWebPushSettingsMock.mockReturnValue({
            busy: false,
            disable: vi.fn(),
            enable: vi.fn(),
            enabled: true,
            error: "",
            featureEnabled: true,
            permission: "granted",
            supported: true,
        });

        render(
            <PushPreferences
                uid="user-1"
                initialEnabled={true}
                pushPreferences={null}
            />
        );

        expect(screen.getByLabelText("A poll opens").checked).toBe(true);
        expect(screen.getByLabelText("A poll completes").checked).toBe(true);
        expect(screen.getByLabelText("A message is sent in global chat").checked).toBe(false);
        expect(screen.getByLabelText("A message is sent in an event chat I am attending").checked).toBe(false);
    });

    it("renders Push Preferences with subscription status and buttons when enabled", () => {
        useWebPushSettingsMock.mockReturnValue({
            busy: false,
            disable: vi.fn(),
            enable: vi.fn(),
            enabled: true,
            error: "",
            featureEnabled: true,
            permission: "granted",
            supported: true,
        });

        render(
            <PushPreferences
                uid="user-1"
                initialEnabled={true}
                pushPreferences={{
                    pollOpens: true,
                    pollCompletes: true,
                    globalChat: false,
                    eventChat: false,
                }}
            />
        );

        expect(screen.getByText(/Status: Enabled/)).toBeTruthy();
        expect(screen.getByText(/Permission: Granted/)).toBeTruthy();
        expect(screen.getByText("Enable Push")).toBeTruthy();
        expect(screen.getByText("Disable Push")).toBeTruthy();
    });

    it("hides notification preferences when not enabled", () => {
        useWebPushSettingsMock.mockReturnValue({
            busy: false,
            disable: vi.fn(),
            enable: vi.fn(),
            enabled: false,
            error: "",
            featureEnabled: true,
            permission: "granted",
            supported: true,
        });

        render(
            <PushPreferences
                uid="user-1"
                initialEnabled={false}
                pushPreferences={{
                    pollOpens: true,
                    pollCompletes: true,
                    globalChat: false,
                    eventChat: false,
                }}
            />
        );

        expect(screen.queryByLabelText("A poll opens")).toBeNull();
        expect(screen.queryByText(/Notify me when:/)).toBeNull();
    });

    it("toggles notification preferences when enabled", async () => {
        useWebPushSettingsMock.mockReturnValue({
            busy: false,
            disable: vi.fn(),
            enable: vi.fn(),
            enabled: true,
            error: "",
            featureEnabled: true,
            permission: "granted",
            supported: true,
        });

        render(
            <PushPreferences
                uid="user-1"
                initialEnabled={true}
                pushPreferences={{
                    pollOpens: true,
                    pollCompletes: false,
                    globalChat: false,
                    eventChat: false,
                }}
            />
        );

        const globalChatCheckbox = screen.getByLabelText("A message is sent in global chat");
        expect(globalChatCheckbox.checked).toBe(false);

        fireEvent.click(globalChatCheckbox);
        expect(globalChatCheckbox.checked).toBe(true);
    });

    it("shows error message when web push encounters an error", () => {
        const errorMessage = "Failed to access push service";
        useWebPushSettingsMock.mockReturnValue({
            busy: false,
            disable: vi.fn(),
            enable: vi.fn(),
            enabled: false,
            error: errorMessage,
            featureEnabled: true,
            permission: "granted",
            supported: true,
        });

        render(
            <PushPreferences
                uid="user-1"
                initialEnabled={false}
                pushPreferences={null}
            />
        );

        expect(screen.getByText(errorMessage)).toBeTruthy();
    });

    it("returns null when feature is not enabled", () => {
        useWebPushSettingsMock.mockReturnValue({
            busy: false,
            disable: vi.fn(),
            enable: vi.fn(),
            enabled: false,
            error: "",
            featureEnabled: false,
            permission: "default",
            supported: false,
        });

        const { container } = render(
            <PushPreferences
                uid="user-1"
                initialEnabled={false}
                pushPreferences={null}
            />
        );

        expect(container.firstChild).toBeNull();
    });
});
