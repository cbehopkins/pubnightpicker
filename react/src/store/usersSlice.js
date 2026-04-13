// @ts-check

import { createSlice } from "@reduxjs/toolkit";

/**
 * @typedef {Object} UserProfile
 * @property {string=} uid
 * @property {string=} name
 * @property {string | null=} photoUrl
 */

/** @typedef {Record<string, UserProfile>} UsersState */

/**
 * @typedef {Object} UserPayload
 * @property {string} uid
 * @property {string=} name
 * @property {string | null=} photoUrl
 */

/** @type {UsersState} */
const initialState = {};

const usersSlice = createSlice({
  name: "users",
  initialState,
  reducers: {
    /** @param {UsersState} state @param {{ payload: UserPayload }} action */
    userAdded(state, action) {
      state[action.payload.uid] = {
        uid: action.payload.uid,
        name: action.payload.name,
        photoUrl: action.payload.photoUrl
      };
    },
    /** @param {UsersState} state @param {{ payload: { uid: string } }} action */
    clearUser(state, action) {
      delete state[action.payload.uid];
    },
    /** @returns {UsersState} */
    clearStore(state) {
      return {};
    },
  },
});

export const { userAdded, clearUser, clearStore } = usersSlice.actions;
export default usersSlice.reducer;
