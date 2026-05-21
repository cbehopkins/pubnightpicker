// @vitest-environment jsdom

import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import PubForm from "./PubForm";

const { navigateMock, navigationStateMock } = vi.hoisted(() => {
    return {
        navigateMock: vi.fn(),
        navigationStateMock: { state: "idle" },
    };
});

vi.mock("react-router-dom", () => {
    return {
        Form: ({ children }) => <form>{children}</form>,
        useNavigate: () => navigateMock,
        useNavigation: () => navigationStateMock,
    };
});

describe("PubForm", () => {
    beforeEach(() => {
        navigateMock.mockReset();
    });

    it("shows recurrence fields for event venues and preloads existing values", () => {
        render(
            <PubForm
                method="patch"
                pub_object={{
                    name: "Cambridge Beer Festival",
                    venueType: "event",
                    banned: true,
                    recurrence: {
                        frequency: "yearly",
                        start_date: "2026-05-01",
                        interval: 1,
                        month: 5,
                        weekday: 2,
                        nth: -1,
                        month_day: 27,
                    },
                    next_occurrence_date: "2026-05-27",
                }}
            />
        );

        // Verify recurrence type is set
        expect(screen.getByLabelText(/recurrence type/i).value).toBe("yearly");
        expect(screen.getByLabelText(/banned/i).checked).toBe(true);

        // Verify yearly mode is set to "weekday" based on existing weekday data
        const weekdayRadio = screen.getByLabelText(/specific weekday/i);
        expect(weekdayRadio.checked).toBe(true);

        // Verify only weekday-related fields are visible for this mode
        expect(screen.getByLabelText(/^month$/i)).toBeInTheDocument();
        expect(screen.getByLabelText(/which week/i)).toBeInTheDocument();

        // Verify preloaded values
        expect(screen.getByLabelText(/^month$/i).value).toBe("5");
        expect(screen.getByLabelText(/which week/i).value).toBe("-1");

        // Verify next occurrence date is displayed with ordinal day formatting
        expect(screen.getByText(/27th May 2026/i)).toBeInTheDocument();
    });
});
