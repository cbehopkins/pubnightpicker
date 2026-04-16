// @ts-check

import { createSlice } from "@reduxjs/toolkit";

/** @typedef {Record<string, true | false | Record<string, boolean> | null | undefined>} RolesMap */

/**
 * @typedef {Object} AuthState
 * @property {string} name
 * @property {string | null} uid
 * @property {boolean} loggedIn
 * @property {string} email
 * @property {RolesMap} roles
 * @property {string | null} photoUrl
 */

/**
 * @typedef {Object} AuthPayload
 * @property {string=} name
 * @property {string | null=} uid
 * @property {string=} email
 * @property {string | null=} photoUrl
 */

/** @type {AuthState} */
const initialState = { name: "", uid: null, loggedIn: false, email: "", roles: {}, photoUrl: null };

const authSlice = createSlice({
  name: "auth",
  initialState,
  reducers: {
    /** @param {AuthState} state @param {{ payload: AuthPayload }} action */
    authAdded(state, action) {
      const uid = typeof action.payload.uid === "string" && action.payload.uid.length > 0
        ? action.payload.uid
        : null;
      const sameUser = uid !== null && state.uid === uid;

      state.name = action.payload.name || "";
      state.uid = uid;
      state.loggedIn = uid !== null;
      state.email = action.payload.email || "";
      state.roles = sameUser ? state.roles : {};
      state.photoUrl = action.payload.photoUrl ?? null;
    },
    /** @param {AuthState} state */
    clearAuth(state) {
      state.name = "";
      state.uid = null;
      state.loggedIn = false;
      state.email = "";
      state.roles = {};
      state.photoUrl = null;
    },
    /** @param {AuthState} state @param {{ payload: RolesMap }} action */
    setRoles(state, action) {
      // Store the full roles object from Firebase
      // action.payload = { admin: { userId: true, ... }, known: { userId: true, ... } }
      state.roles = action.payload;
    },
  },
});

export const { authAdded, clearAuth, setRoles } = authSlice.actions;
export default authSlice.reducer;
