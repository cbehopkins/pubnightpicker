import { configureStore } from '@reduxjs/toolkit'
import authReducer from "./authSlice"
import pollsReducer from "./pollsSlice"
import usersReducer from "./usersSlice"
import groupReducer from "./groupSlice"
export const store = configureStore({
  reducer: {
    auth: authReducer,
    polls: pollsReducer,
    users: usersReducer,
    group: groupReducer,
  }
})

/** @typedef {ReturnType<typeof store.getState>} RootState */
/** @typedef {typeof store.dispatch} AppDispatch */
