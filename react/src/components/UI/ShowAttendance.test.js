// @vitest-environment jsdom

import { render, screen, cleanup } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

afterEach(cleanup);
import ShowAttendance from "./ShowAttendance";

vi.mock("./Modal", () => {
    return {
        default: ({ children }) => <div>{children}</div>,
    };
});

vi.mock("../../hooks/useUsers", () => {
    return {
        default: () => ({
            "user-1": { name: "Alex", votesVisible: true },
            "user-2": { name: "Sam", votesVisible: false },
            "user-3": { name: "Jamie" },
        }),
    };
});

describe("ShowAttendance", () => {
    it("renders combined table with only columns that have data", () => {
        render(
            <ShowAttendance
                voters={["user-1"]}
                canCome={["user-2"]}
                cannotCome={[]}
            />,
        );

        expect(screen.getByText("Voted")).toBeTruthy();
        expect(screen.getByText("Can Come")).toBeTruthy();
        expect(screen.queryByText("Cannot Come")).toBeNull();
        expect(screen.getByText("Alex")).toBeTruthy();
        expect(screen.getByText("Sam")).toBeTruthy();
    });

    it("shows Cannot Come column only when there are responses", () => {
        render(
            <ShowAttendance
                voters={[]}
                canCome={[]}
                cannotCome={["user-3"]}
            />,
        );

        expect(screen.queryByText("Voted")).toBeNull();
        expect(screen.queryByText("Can Come")).toBeNull();
        expect(screen.getByText("Cannot Come")).toBeTruthy();
        expect(screen.getByText("Jamie")).toBeTruthy();
    });

    it("shows one row per person even when they appear in multiple columns", () => {
        render(
            <ShowAttendance
                voters={["user-1"]}
                canCome={["user-1"]}
                cannotCome={[]}
            />,
        );

        expect(screen.getByText("Voted")).toBeTruthy();
        expect(screen.getByText("Can Come")).toBeTruthy();
        // Alex should appear exactly once as a row name
        expect(screen.getAllByText("Alex")).toHaveLength(1);
    });

    it("hides vote identity when votesVisible is false", () => {
        render(
            <ShowAttendance
                voters={["user-2"]}
                canCome={[]}
                cannotCome={[]}
            />,
        );

        expect(screen.queryByText("Sam")).toBeNull();
        expect(screen.queryByText("Voted")).toBeNull();
    });

    it("defaults to visible when votesVisible is missing", () => {
        render(
            <ShowAttendance
                voters={["user-3"]}
                canCome={[]}
                cannotCome={[]}
            />,
        );

        expect(screen.getByText("Jamie")).toBeTruthy();
        expect(screen.getByText("Voted")).toBeTruthy();
    });
});
