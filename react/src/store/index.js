import { configureStore } from '@reduxjs/toolkit'
import authReducer from "./authSlice"
import pollsReducer from "./pollsSlice"
import usersReducer from "./usersSlice"
export const store = configureStore({
  reducer: {
    auth: authReducer,
    polls: pollsReducer,
    users: usersReducer,
  }
})