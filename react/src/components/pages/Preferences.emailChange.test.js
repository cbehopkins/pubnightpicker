// @vitest-environment jsdom

import React from "react";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const {
    useAuthStateMock,
    useSelectorMock,
    requestLoginEmailChangeMock,
    reauthenticatePasswordUserMock,
    notifyInfoMock,
    notifyErrorMock,
    getAuthMock,
    updatePasswordMock,
    navigateMock,
} = vi.hoisted(() => {
    return {
        useAuthStateMock: vi.fn(),
        useSelectorMock: vi.fn(),
        requestLoginEmailChangeMock: vi.fn(),
        reauthenticatePasswordUserMock: vi.fn(),
        notifyInfoMock: vi.fn(),
        notifyErrorMock: vi.fn(),
        getAuthMock: vi.fn(),
        updatePasswordMock: vi.fn(async () => undefined),
        navigateMock: vi.fn(),
    };
});

vi.mock("react-firebase-hooks/auth", () => {
    return {
        useAuthState: useAuthStateMock,
    };
});

vi.mock("react-redux", () => {
    return {
        useSelector: useSelectorMock,
    };
});

vi.mock("../../firebase", () => {
    return {
        db: {},
        requestLoginEmailChange: requestLoginEmailChangeMock,
        reauthenticatePasswordUser: reauthenticatePasswordUserMock,
    };
});

vi.mock("firebase/auth", () => {
    return {
        getAuth: getAuthMock,
        updatePassword: updatePasswordMock,
    };
});

vi.mock("../../utils/notify", () => {
    return {
        notifyInfo: notifyInfoMock,
        notifyError: notifyErrorMock,
    };
});

vi.mock("../../utils/themeMode", () => {
    return {
        applyThemeMode: vi.fn(),
        getStoredThemeMode: vi.fn(() => "auto"),
        setStoredThemeMode: vi.fn(),
        subscribeToSystemThemeChanges: vi.fn(() => () => undefined),
    };
});

vi.mock("./PreferencesForm", () => {
    return {
        default: () => <div data-testid="preferences-form" />,
    };
});

vi.mock("../UI/Button", () => {
    return {
        default: ({ children, onClick, ...rest }) => (
            <button type="button" onClick={onClick} {...rest}>{children}</button>
        ),
    };
});

vi.mock("../UI/TextModal", () => {
    const TextModal = ({ title, detail, name, confirm_text, cancel_text, on_confirm, on_cancel }) => {
        const [value, setValue] = React.useState("");
        return (
            <div data-testid={`text-modal-${name}`}>
                <h2>{title}</h2>
                <label htmlFor={`input-${name}`}>{detail}</label>
                <input
                    id={`input-${name}`}
                    value={value}
                    onChange={(event) => setValue(event.target.value)}
                />
                <button
                    type="button"
                    onClick={(event) => on_confirm(event, { current: { value } })}
                >
                    {confirm_text}
                </button>
                <button type="button" onClick={on_cancel}>{cancel_text}</button>
            </div>
        );
    };
    return { default: TextModal };
});

vi.mock("../UI/ConfirmModal", () => {
    return {
        default: ({ title, detail, confirm_text, on_confirm }) => (
            <div data-testid="confirm-modal">
                <h2>{title}</h2>
                <div>{detail}</div>
                <button type="button" onClick={on_confirm}>{confirm_text || "Ok"}</button>
            </div>
        ),
    };
});

vi.mock("react-router-dom", async () => {
    const actual = await vi.importActual("react-router-dom");
    return {
        ...actual,
        useNavigate: () => navigateMock,
    };
});

import Preferences from "./Preferences";

function renderPreferences() {
    return render(
        <MemoryRouter>
            <Preferences />
        </MemoryRouter>
    );
}

describe("Preferences login email change flow", () => {
    beforeEach(() => {
        useSelectorMock.mockReset();
        useAuthStateMock.mockReset();
        requestLoginEmailChangeMock.mockReset();
        reauthenticatePasswordUserMock.mockReset();
        notifyInfoMock.mockReset();
        notifyErrorMock.mockReset();
        getAuthMock.mockReset();
        updatePasswordMock.mockReset();
        navigateMock.mockReset();

        useSelectorMock.mockReturnValue({});

        const currentUser = {
            uid: "user-1",
            email: "old@example.com",
            providerData: [{ providerId: "password" }],
        };

        useAuthStateMock.mockReturnValue([currentUser, false]);
        getAuthMock.mockReturnValue({
            currentUser,
        });
    });

    afterEach(() => {
        cleanup();
    });

    it("sends a verification request and shows success info", async () => {
        requestLoginEmailChangeMock.mockResolvedValueOnce({
            ok: true,
            email: "new@example.com",
        });

        renderPreferences();

        fireEvent.click(screen.getByRole("button", { name: "Change Login Email" }));
        fireEvent.change(screen.getByLabelText("New login email"), {
            target: { value: "New@Example.com" },
        });
        fireEvent.click(screen.getByRole("button", { name: "Send Verification" }));

        await waitFor(() => {
            expect(requestLoginEmailChangeMock).toHaveBeenCalledWith("New@Example.com");
        });

        expect(notifyInfoMock).toHaveBeenCalledWith(
            "Verification email sent. Confirm it to finish updating your login email."
        );
    });

    it("requires reauthentication when session is stale and retries email request", async () => {
        requestLoginEmailChangeMock
            .mockResolvedValueOnce({
                ok: false,
                requiresRecentLogin: true,
                email: "new@example.com",
            })
            .mockResolvedValueOnce({
                ok: true,
                email: "new@example.com",
            });
        reauthenticatePasswordUserMock.mockResolvedValueOnce({ ok: true });

        renderPreferences();

        fireEvent.click(screen.getByRole("button", { name: "Change Login Email" }));
        fireEvent.change(screen.getByLabelText("New login email"), {
            target: { value: "New@Example.com" },
        });
        fireEvent.click(screen.getByRole("button", { name: "Send Verification" }));

        await waitFor(() => {
            expect(screen.getByLabelText("Reauthentication needed. Please re-enter your current password")).toBeTruthy();
        });

        fireEvent.change(
            screen.getByLabelText("Reauthentication needed. Please re-enter your current password"),
            { target: { value: "oldpass" } }
        );
        fireEvent.click(screen.getByRole("button", { name: "submit" }));

        await waitFor(() => {
            expect(reauthenticatePasswordUserMock).toHaveBeenCalledWith("oldpass");
            expect(requestLoginEmailChangeMock).toHaveBeenCalledTimes(2);
            expect(requestLoginEmailChangeMock).toHaveBeenNthCalledWith(2, "new@example.com");
        });
    });

    it("hides change login email for non-password providers", () => {
        const googleUser = {
            uid: "user-1",
            email: "old@example.com",
            providerData: [{ providerId: "google.com" }],
        };

        useAuthStateMock.mockReturnValue([googleUser, false]);
        getAuthMock.mockReturnValue({ currentUser: googleUser });

        renderPreferences();

        expect(screen.queryByRole("button", { name: "Change Login Email" })).toBeNull();
    });
});
