// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
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

beforeEach(() => { vi.clearAllMocks(); authMock.currentUser = { uid: "creator-user" }; });
afterEach(() => { cleanup(); vi.restoreAllMocks(); });

it("records the creator in the initial poll write", async () => {
    const { container } = render(<NewPoll polls={new Set()} />);
    fireEvent.change(container.querySelector("input"), { target: { value: "2026-10-15" } });
    fireEvent.click(screen.getByRole("button", { name: "Add Poll" }));
    await waitFor(() => expect(addDocMock).toHaveBeenCalledWith("polls", { date: "2026-10-15", completed: false, createdByUid: "creator-user" }));
});

it("does not create a poll without an authenticated actor", async () => {
    authMock.currentUser = null;
    const error = vi.spyOn(console, "error").mockImplementation(() => { });
    const { container } = render(<NewPoll polls={new Set()} />);
    fireEvent.change(container.querySelector("input"), { target: { value: "2026-10-15" } });
    fireEvent.click(screen.getByRole("button", { name: "Add Poll" }));
    await waitFor(() => expect(error).toHaveBeenCalled());
    expect(addDocMock).not.toHaveBeenCalled();
});
