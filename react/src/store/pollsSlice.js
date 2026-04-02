// @ts-check

import { createSlice } from "@reduxjs/toolkit";

/** @typedef {Record<string, unknown>} PollValue */

/**
 * @typedef {Object} PollEntry
 * @property {string} id
 * @property {PollValue} poll
 */

/** @typedef {PollEntry[]} PollsState */

/**
 * @typedef {Object} PollPayload
 * @property {string} id
 * @property {PollValue} poll
 */

/** @type {PollsState} */
const initialState = [];

const pollsSlice = createSlice({
  name: "polls",
  initialState,
  reducers: {
    /** @param {PollsState} state @param {{ payload: PollPayload }} action */
    pollAdded(state, action) {
      const id = action.payload.id;
      state.push({
        id,
        poll: action.payload.poll,
      });
    },
    /** @param {PollsState} state @param {{ payload: PollPayload }} action */
    pollModified(state, action) {
      const id = action.payload.id;
      const index = state.findIndex((v) => (v.id === id));
      if (index < 0) {
        console.error("Unable to find id in state, mod", id, state)
        return
      }
      state[index] = {
        id,
        poll: action.payload.poll,
      };
    },
    /** @param {PollsState} state @param {{ payload: { id: string } }} action */
    pollRemoved(state, action) {
      const id = action.payload.id;
      const index = state.findIndex((v) => (v.id === id));
      if (index < 0) {
        console.error("Unable to find id in state, rm", id, state)
        return
      }
      state.splice(index, 1);
    }
  },
});

export const { pollAdded, pollModified, pollRemoved } = pollsSlice.actions;
export default pollsSlice.reducer;
