// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from "vitest";

// ─── Firestore mock ───────────────────────────────────────────────────────────

const { onSnapshotMock, queryMock, collectionMock, orderByMock, limitMock, whereMock } =
    vi.hoisted(() => ({
        onSnapshotMock: vi.fn(),
        queryMock: vi.fn((...args) => ({ _args: args })),
        collectionMock: vi.fn((db, name) => ({ _col: name })),
        orderByMock: vi.fn((field, dir) => ({ _orderBy: [field, dir] })),
        limitMock: vi.fn((n) => ({ _limit: n })),
        whereMock: vi.fn((field, op, val) => ({ _where: [field, op, val] })),
    }));

vi.mock("firebase/firestore", () => ({
    query: queryMock,
    collection: collectionMock,
    orderBy: orderByMock,
    limit: limitMock,
    where: whereMock,
    onSnapshot: onSnapshotMock,
}));

vi.mock("../../firebase", () => ({ db: {} }));
vi.mock("../../hooks/useUsers", () => ({ default: () => ({}) }));
vi.mock("./SendMessage", () => ({ default: () => null }));
vi.mock("./Message", () => ({ default: () => null }));
vi.mock("react-redux", () => ({ useSelector: vi.fn() }));

// ─── React / testing ─────────────────────────────────────────────────────────

import React from "react";
import { render } from "@testing-library/react";
import ChatBox from "./ChatBox";

// ─── Tests ───────────────────────────────────────────────────────────────────

beforeEach(() => {
    vi.clearAllMocks();
});

describe("ChatBox — query construction", () => {
    it("uses event-scoped where clauses for event scope", () => {
        onSnapshotMock.mockReturnValue(() => { });
        render(<ChatBox scope={{ scopeType: "event", scopeId: "poll-1" }} />);

        expect(whereMock).toHaveBeenCalledWith("scopeType", "==", "event");
        expect(whereMock).toHaveBeenCalledWith("scopeId", "==", "poll-1");
        expect(orderByMock).toHaveBeenCalledWith("createdAt", "desc");
        expect(limitMock).toHaveBeenCalledWith(50);
    });

    it("uses scopeType and scopeId where clauses for global scope", () => {
        onSnapshotMock.mockReturnValue(() => { });
        render(<ChatBox scope={{ scopeType: "global", scopeId: "main" }} />);

        expect(whereMock).toHaveBeenCalledWith("scopeType", "==", "global");
        expect(whereMock).toHaveBeenCalledWith("scopeId", "==", "main");
        expect(orderByMock).toHaveBeenCalledWith("createdAt", "desc");
        expect(limitMock).toHaveBeenCalledWith(50);
    });

    it("defaults to global scope when no scope prop is provided", () => {
        onSnapshotMock.mockReturnValue(() => { });
        render(<ChatBox />);

        expect(whereMock).toHaveBeenCalledWith("scopeType", "==", "global");
        expect(whereMock).toHaveBeenCalledWith("scopeId", "==", "main");
        expect(orderByMock).toHaveBeenCalledWith("createdAt", "desc");
    });

    it("re-runs query when scope changes", () => {
        onSnapshotMock.mockReturnValue(() => { });
        const { rerender } = render(
            <ChatBox scope={{ scopeType: "event", scopeId: "poll-1" }} />
        );
        const firstCallCount = onSnapshotMock.mock.calls.length;

        rerender(<ChatBox scope={{ scopeType: "event", scopeId: "poll-2" }} />);

        expect(onSnapshotMock.mock.calls.length).toBeGreaterThan(firstCallCount);
        const lastWhereCall = whereMock.mock.calls.find(
            ([, , val]) => val === "poll-2"
        );
        expect(lastWhereCall).toBeDefined();
    });
});

describe("ChatBox — scope defaults", () => {
    it("treats missing scope as global/main", () => {
        onSnapshotMock.mockReturnValue(() => { });
        render(<ChatBox />);

        expect(whereMock).toHaveBeenCalledWith("scopeType", "==", "global");
        expect(whereMock).toHaveBeenCalledWith("scopeId", "==", "main");
    });

    it("treats scope without scopeId as global/main", () => {
        onSnapshotMock.mockReturnValue(() => { });
        render(<ChatBox scope={{}} />);

        expect(whereMock).toHaveBeenCalledWith("scopeType", "==", "global");
        expect(whereMock).toHaveBeenCalledWith("scopeId", "==", "main");
    });
});
