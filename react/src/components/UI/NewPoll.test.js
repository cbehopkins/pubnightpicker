// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { Provider } from "react-redux";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

const { authMock, addDocMock, setDocMock, requestLastOrdersMock, notifyErrorMock } = vi.hoisted(() => ({
    authMock: { currentUser: { uid: "creator-user" } },
    addDocMock: vi.fn(async () => ({ id: "created-poll" })),
    setDocMock: vi.fn(async () => undefined),
    requestLastOrdersMock: vi.fn(async () => ({ id: "created-poll", date: "2026-10-15" })),
    notifyErrorMock: vi.fn(),
}));
vi.mock("../../firebase", () => ({ auth: authMock, db: {} }));
vi.mock("firebase/firestore", () => ({
    addDoc: addDocMock, setDoc: setDocMock,
    collection: (_db, name) => name, doc: (_db, name, id) => `${name}/${id}`,
}));
vi.mock("../../dbtools/pollActionAudit", () => ({ logPollActionAudit: vi.fn(), POLL_ACTION_CREATE: "create" }));
vi.mock("../../utils/lastOrdersApi", () => ({ requestLastOrders: requestLastOrdersMock }));
vi.mock("../../utils/notify", () => ({ notifyError: notifyErrorMock }));
vi.mock("../../permissions", () => ({
    getUserFacingErrorMessage: (error, fallback) => error?.message || fallback,
}));
vi.mock("../UI/Button", () => ({ default: ({ children, ...props }) => <button {...props}>{children}</button> }));
import NewPoll from "./NewPoll";
import { logPollActionAudit } from "../../dbtools/pollActionAudit";
import { store } from "../../store";
import { setGroupId } from "../../store/groupSlice";

function renderNewPoll() {
    return render(<Provider store={store}><NewPoll polls={new Set()} /></Provider>);
}

beforeEach(() => {
    vi.clearAllMocks();
    requestLastOrdersMock.mockResolvedValue({ id: "created-poll", date: "2026-10-15" });
    authMock.currentUser = { uid: "creator-user" };
    store.dispatch(setGroupId("opaque-group-id"));
});
afterEach(() => { cleanup(); vi.restoreAllMocks(); });

it("creates a Meet through the Last Orders API", async () => {
    const { container } = renderNewPoll();
    fireEvent.change(container.querySelector("input"), { target: { value: "2026-10-15" } });
    fireEvent.click(screen.getByRole("button", { name: "Add Poll" }));
    await waitFor(() => expect(requestLastOrdersMock).toHaveBeenCalledWith(
        "api/meets/opaque-group-id",
        { method: "POST", body: { date: "2026-10-15" } },
    ));
    expect(addDocMock).not.toHaveBeenCalled();
    expect(setDocMock).not.toHaveBeenCalled();
    expect(logPollActionAudit).not.toHaveBeenCalled();
});

it("does not create a poll when the resolved Group is unavailable", async () => {
    store.dispatch(setGroupId(null));
    const { container } = renderNewPoll();
    fireEvent.change(container.querySelector("input"), { target: { value: "2026-10-15" } });
    fireEvent.click(screen.getByRole("button", { name: "Add Poll" }));
    await waitFor(() => expect(notifyErrorMock).toHaveBeenCalledWith("No resolved Group for poll creation."));
    expect(requestLastOrdersMock).not.toHaveBeenCalled();
    expect(addDocMock).not.toHaveBeenCalled();
});

it("shows an uncertain creation outcome and does not retry", async () => {
    requestLastOrdersMock.mockRejectedValue({ code: "connection_error" });
    const { container } = renderNewPoll();
    fireEvent.change(container.querySelector("input"), { target: { value: "2026-10-15" } });
    fireEvent.click(screen.getByRole("button", { name: "Add Poll" }));
    await waitFor(() => expect(notifyErrorMock).toHaveBeenCalledWith(
        "Meet creation outcome is uncertain. Refresh the open Meets view before retrying.",
    ));
    expect(requestLastOrdersMock).toHaveBeenCalledTimes(1);
    expect(addDocMock).not.toHaveBeenCalled();
});
