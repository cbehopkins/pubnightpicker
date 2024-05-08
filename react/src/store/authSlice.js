import { createSlice } from "@reduxjs/toolkit";

const authSlice = createSlice({
  name: "auth",
  initialState: { name: "", uid: 0, loggedIn: false, email: "", admin: false, known: false, photoUrl: null },
  reducers: {
    authAdded(state, action) {
      state.name = action.payload.name;
      state.uid = action.payload.uid;
      state.loggedIn = Boolean(action.payload.uid);
      state.email = action.payload.email;
      state.admin = false;
      state.known = false;
      state.photoUrl = action.payload.photoUrl
    },
    clearAuth(state) {
      state.name = "";
      state.uid = 0;
      state.loggedIn = false;
      state.email = "";
      state.admin = false;
      state.known = false;
      state.photoUrl = null;
    },
    setRoles(state, action) {
      if ("admin" in action.payload) {
        state.admin = action.payload.admin;
      }
      if ("known" in action.payload) {
        state.known = action.payload.known;
      }
    },
  },
});

export const { authAdded, clearAuth, setRoles } = authSlice.actions;
export default authSlice.reducer;
