import { createSlice } from "@reduxjs/toolkit";

const authSlice = createSlice({
  name: "auth",
  initialState: { name: "", uid: 0, loggedIn: false, email: "", roles: {}, photoUrl: null },
  reducers: {
    authAdded(state, action) {
      state.name = action.payload.name;
      state.uid = action.payload.uid;
      state.loggedIn = Boolean(action.payload.uid);
      state.email = action.payload.email;
      state.roles = {};
      state.photoUrl = action.payload.photoUrl
    },
    clearAuth(state) {
      state.name = "";
      state.uid = 0;
      state.loggedIn = false;
      state.email = "";
      state.roles = {};
      state.photoUrl = null;
    },
    setRoles(state, action) {
      // Store the full roles object from Firebase
      // action.payload = { admin: { userId: true, ... }, known: { userId: true, ... } }
      state.roles = action.payload;
    },
  },
});

export const { authAdded, clearAuth, setRoles } = authSlice.actions;
export default authSlice.reducer;
