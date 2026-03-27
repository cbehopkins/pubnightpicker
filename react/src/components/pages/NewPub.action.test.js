import { beforeEach, describe, expect, it, vi } from "vitest";
import { action } from "./NewPub";

const {
    addNewPubMock,
    modifyPubMock,
    notifyErrorMock,
} = vi.hoisted(() => {
    return {
        addNewPubMock: vi.fn(),
        modifyPubMock: vi.fn(),
        notifyErrorMock: vi.fn(),
    };
});

vi.mock("../../dbtools/pubs", () => {
    return {
        addNewPub: addNewPubMock,
        modifyPub: modifyPubMock,
    };
});

vi.mock("../../utils/notify", () => {
    return {
        notifyError: notifyErrorMock,
    };
});

function createRequest(method, formValues) {
    return new Request("http://localhost/pubs", {
        method,
        body: new URLSearchParams(formValues),
    });
}

describe("NewPub action", () => {
    beforeEach(() => {
        addNewPubMock.mockReset();
        modifyPubMock.mockReset();
        notifyErrorMock.mockReset();
    });

    it("creates a pub and redirects on POST", async () => {
        const response = await action({
            request: createRequest("POST", {
                name: "Test Pub",
                web_site: "https://example.com",
                map: "https://maps.example.com",
                address: "1 Test Street",
                pubImage: "https://img.example.com/pub.png",
            }),
            params: {},
        });

        expect(addNewPubMock).toHaveBeenCalledTimes(1);
        expect(modifyPubMock).not.toHaveBeenCalled();
        expect(response.status).toBe(302);
        expect(response.headers.get("Location")).toBe("/pubs");
    });

    it("modifies a pub and redirects on PATCH", async () => {
        const response = await action({
            request: createRequest("PATCH", {
                name: "Updated Pub",
            }),
            params: { pubId: "pub-123" },
        });

        expect(modifyPubMock).toHaveBeenCalledTimes(1);
        expect(modifyPubMock).toHaveBeenCalledWith("pub-123", expect.any(Object));
        expect(response.status).toBe(302);
        expect(response.headers.get("Location")).toBe("/pubs");
    });

    it("shows a user-facing error and aborts redirect on failure", async () => {
        addNewPubMock.mockRejectedValueOnce(new Error("backend exploded"));

        const result = await action({
            request: createRequest("POST", {
                name: "Bad Pub",
            }),
            params: {},
        });

        expect(notifyErrorMock).toHaveBeenCalledTimes(1);
        expect(result).toBeNull();
    });
});
