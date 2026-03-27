// @vitest-environment jsdom

import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import ShowAttendance from "./ShowAttendance";

vi.mock("./Modal", () => {
    return {
        default: ({ children }) => <div>{children}</div>,
    };
});

vi.mock("../../hooks/useUsers", () => {
    return {
        default: () => ({
            "user-1": { name: "Alex" },
            "user-2": { name: "Sam" },
            "user-3": { name: "Jamie" },
        }),
    };
});

describe("ShowAttendance", () => {
    it("renders voters and attendance groupings", () => {
        render(
            <ShowAttendance
                voters={["user-1"]}
                canCome={["user-2"]}
                cannotCome={[]}
            />,
        );

        expect(screen.getByText("Current Voters")).toBeTruthy();
        expect(screen.getByText("Can Come")).toBeTruthy();
        expect(screen.queryByText("Cannot Come")).toBeNull();
        expect(screen.getByText("Alex")).toBeTruthy();
        expect(screen.getByText("Sam")).toBeTruthy();
        expect(screen.queryByText("No responses yet")).toBeNull();
    });
});
