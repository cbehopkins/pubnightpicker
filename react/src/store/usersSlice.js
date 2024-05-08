import { createSlice } from "@reduxjs/toolkit";

const usersSlice = createSlice({
  name: "users",
  initialState: {},
  reducers: {
    userAdded(state, action) {
      state[action.payload.uid] = {
        name: action.payload.name,
        email: action.payload.email,
        votesVisible: action.payload.votesVisible,
        photoUrl: action.payload.photoUrl
      };
    },
    clearUser(state, action) {
      delete state[action.payload.uid];
    },
    clearStore(state) {
      state = {};
    },
  },
});

export const { userAdded, clearUser, clearStore } = usersSlice.actions;
export default usersSlice.reducer;
