// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { Provider } from "react-redux";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

const { authMock, addDocMock, setDocMock } = vi.hoisted(() => ({
    authMock: { currentUser: { uid: "creator-user" } },
    addDocMock: vi.fn(async () => ({ id: "created-poll" })),
    setDocMock: vi.fn(async () => undefined),
}));
vi.mock("../../firebase", () => ({ auth: authMock, db: {} }));
vi.mock("firebase/firestore", () => ({
    addDoc: addDocMock, setDoc: setDocMock,
    collection: (_db, name) => name, doc: (_db, name, id) => `${name}/${id}`,
}));
vi.mock("../../dbtools/pollActionAudit", () => ({ logPollActionAudit: vi.fn(), POLL_ACTION_CREATE: "create" }));
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
    authMock.currentUser = { uid: "creator-user" };
    store.dispatch(setGroupId("opaque-group-id"));
});
afterEach(() => { cleanup(); vi.restoreAllMocks(); });

it("records the creator in the initial poll write", async () => {
    const { container } = renderNewPoll();
    fireEvent.change(container.querySelector("input"), { target: { value: "2026-10-15" } });
    fireEvent.click(screen.getByRole("button", { name: "Add Poll" }));
    await waitFor(() => expect(addDocMock).toHaveBeenCalledWith("polls", { date: "2026-10-15", completed: false, createdByUid: "creator-user", groupId: "opaque-group-id" }));
    await waitFor(() => expect(logPollActionAudit).toHaveBeenCalledWith("create", { pollId: "created-poll", pollDate: "2026-10-15" }));
    expect(setDocMock).toHaveBeenCalledWith("votes/created-poll", { any: [] });
    expect(setDocMock).toHaveBeenCalledWith("attendance/created-poll", {});
});

it("does not create a poll when the resolved Group is unavailable", async () => {
    store.dispatch(setGroupId(null));
    const error = vi.spyOn(console, "error").mockImplementation(() => { });
    const { container } = renderNewPoll();
    fireEvent.change(container.querySelector("input"), { target: { value: "2026-10-15" } });
    fireEvent.click(screen.getByRole("button", { name: "Add Poll" }));
    await waitFor(() => expect(error).toHaveBeenCalled());
    expect(addDocMock).not.toHaveBeenCalled();
});

it("does not create a poll without an authenticated actor", async () => {
    authMock.currentUser = null;
    const error = vi.spyOn(console, "error").mockImplementation(() => { });
    const { container } = renderNewPoll();
    fireEvent.change(container.querySelector("input"), { target: { value: "2026-10-15" } });
    fireEvent.click(screen.getByRole("button", { name: "Add Poll" }));
    await waitFor(() => expect(error).toHaveBeenCalled());
    expect(addDocMock).not.toHaveBeenCalled();
});
