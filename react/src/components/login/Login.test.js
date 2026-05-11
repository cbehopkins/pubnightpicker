// @vitest-environment jsdom

import React from "react";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const {
  useAuthStateMock,
  navigateMock,
  logInWithEmailAndPasswordMock,
  signInWithGoogleMock,
} = vi.hoisted(() => {
  return {
    useAuthStateMock: vi.fn(),
    navigateMock: vi.fn(),
    logInWithEmailAndPasswordMock: vi.fn(),
    signInWithGoogleMock: vi.fn(),
  };
});

vi.mock("react-firebase-hooks/auth", () => {
  return {
    useAuthState: useAuthStateMock,
  };
});

vi.mock("../../firebase", () => {
  return {
    auth: {},
    logInWithEmailAndPassword: logInWithEmailAndPasswordMock,
    signInWithGoogle: signInWithGoogleMock,
  };
});

vi.mock("react-router-dom", async () => {
  const actual = await vi.importActual("react-router-dom");
  return {
    ...actual,
    useNavigate: () => navigateMock,
  };
});

import Login from "./Login";

describe("Login form submit behavior", () => {
  beforeEach(() => {
    useAuthStateMock.mockReset();
    navigateMock.mockReset();
    logInWithEmailAndPasswordMock.mockReset();
    signInWithGoogleMock.mockReset();
    useAuthStateMock.mockReturnValue([null, false]);
  });

  afterEach(() => {
    cleanup();
  });

  it("submits email/password when Enter is pressed in the password field", async () => {
    render(
      <MemoryRouter>
        <Login />
      </MemoryRouter>
    );

    fireEvent.change(screen.getByLabelText("Email Address"), {
      target: { value: "person@example.com" },
    });
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "secret-pass" },
    });

    const passwordInput = screen.getByLabelText("Password");
    passwordInput.focus();
    await userEvent.type(passwordInput, "{enter}");

    expect(logInWithEmailAndPasswordMock).toHaveBeenCalledTimes(1);
    expect(logInWithEmailAndPasswordMock).toHaveBeenCalledWith("person@example.com", "secret-pass");
  });
});
