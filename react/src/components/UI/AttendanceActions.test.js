// @vitest-environment jsdom

import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import AttendanceActions from "./AttendanceActions";

afterEach(cleanup);

it("shows a pending state and then the observed response", async () => {
    let finish;
    const onSetStatus = vi.fn(() => new Promise((resolve) => { finish = resolve; }));
    const props = { canComeSelected: false, cannotComeSelected: false, onSetStatus, onClear: vi.fn() };
    const { rerender } = render(<AttendanceActions {...props} />);

    fireEvent.click(screen.getByRole("button", { name: "Can come" }));
    expect(screen.getByRole("button", { name: "Saving..." }).disabled).toBe(true);
    expect(screen.getByRole("button", { name: "Cannot come" }).disabled).toBe(true);
    expect(onSetStatus).toHaveBeenCalledWith("canCome");

    rerender(<AttendanceActions {...props} canComeSelected canComeSelectedLabel="Can come confirmed" />);
    await act(async () => finish());
    expect(screen.getByRole("button", { name: "Can come confirmed" }).disabled).toBe(false);
});

it("returns to the observed state after a rejected action", async () => {
    const onClear = vi.fn(async () => { throw new Error("connection lost"); });
    render(<AttendanceActions canComeSelected cannotComeSelected={false} onSetStatus={vi.fn()} onClear={onClear} clearMode="button" />);

    fireEvent.click(screen.getByRole("button", { name: "Clear response" }));
    await act(async () => { });
    expect(screen.getByRole("button", { name: "Clear response" }).disabled).toBe(false);
    expect(onClear).toHaveBeenCalledOnce();
});
