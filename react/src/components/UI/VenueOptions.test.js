// @vitest-environment jsdom

import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import VenueOptions from "./VenueOptions";

describe("VenueOptions", () => {
    it("renders banned venues with highlighted text color", () => {
        render(
            <VenueOptions
                venues={{
                    a: { name: "Anchor Inn" },
                    b: { name: "Broken Keg", banned: true },
                }}
                selectVenueHandler={vi.fn()}
            />
        );

        const bannedOption = screen.getByRole("option", { name: "Broken Keg" });
        const regularOption = screen.getByRole("option", { name: "Anchor Inn" });

        expect(bannedOption).toHaveStyle({ color: "#8a4b00" });
        expect(regularOption.getAttribute("style")).toBeNull();
    });

    it("still keeps venues sorted by name", () => {
        render(
            <VenueOptions
                venues={{
                    z: { name: "Zebra Arms", banned: true },
                    a: { name: "Anchor Inn" },
                }}
                selectVenueHandler={vi.fn()}
            />
        );

        const options = screen.getAllByRole("option");
        expect(options[1].textContent).toBe("Anchor Inn");
        expect(options[2].textContent).toBe("Zebra Arms");
    });
});
