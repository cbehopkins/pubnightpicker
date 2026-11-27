// @vitest-environment jsdom

import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import useAutopopulateAction from "./useAutopopulateAction";

const { autopopulateMeetMock, notifyInfoMock, notifyErrorMock } = vi.hoisted(() => ({
    autopopulateMeetMock: vi.fn(), notifyInfoMock: vi.fn(), notifyErrorMock: vi.fn(),
}));

vi.mock("../dbtools/polls", () => ({ autopopulateMeet: autopopulateMeetMock }));
vi.mock("../utils/notify", () => ({ notifyInfo: notifyInfoMock, notifyError: notifyErrorMock }));

describe("useAutopopulateAction", () => {
    beforeEach(() => { vi.clearAllMocks(); autopopulateMeetMock.mockResolvedValue({ additions: [], outcome: "exhausted", exhausted: true }); });

    it("sends one on-demand request and reports an empty result", async () => {
        const { result } = renderHook(() => useAutopopulateAction("meet-1"));
        await act(async () => result.current.handleAutopopulate());
        expect(autopopulateMeetMock).toHaveBeenCalledTimes(1);
        expect(autopopulateMeetMock).toHaveBeenCalledWith("meet-1");
        expect(notifyInfoMock).toHaveBeenCalledWith("No candidates were available to add.");
    });

    it("reports confirmed partial additions after an abort", async () => {
        autopopulateMeetMock.mockResolvedValue({ additions: [{ id: "venue-1" }], outcome: "partial", uncertain: false });
        const { result } = renderHook(() => useAutopopulateAction("meet-1"));
        await act(async () => result.current.handleAutopopulate());
        expect(notifyErrorMock).toHaveBeenCalledWith("Autopopulate stopped after 1 confirmed addition. Confirmed: venue-1.");
    });

    it("distinguishes an uncertain outcome", async () => {
        autopopulateMeetMock.mockResolvedValue({ additions: [], outcome: "uncertain", uncertain: true });
        const { result } = renderHook(() => useAutopopulateAction("meet-1"));
        await act(async () => result.current.handleAutopopulate());
        expect(notifyErrorMock).toHaveBeenCalledWith("The result is uncertain. No additions were confirmed. No later candidate was attempted.");
    });

    it("prevents overlapping submissions", async () => {
        let finish;
        autopopulateMeetMock.mockImplementation(() => new Promise((resolve) => { finish = resolve; }));
        const { result } = renderHook(() => useAutopopulateAction("meet-1"));
        let first;
        act(() => { first = result.current.handleAutopopulate(); });
        act(() => { result.current.handleAutopopulate(); });
        expect(autopopulateMeetMock).toHaveBeenCalledTimes(1);
        await act(async () => { finish({ additions: [], outcome: "exhausted", exhausted: true }); await first; });
    });
});
