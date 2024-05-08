import { createSlice } from "@reduxjs/toolkit";

const pollsSlice = createSlice({
  name: "polls",
  initialState: [],
  reducers: {
    pollAdded(state, action) {
      const id = action.payload.id;
      state.push({
        id,
        poll: action.payload.poll,
      });
    },
    pollModified(state, action) {
      const id = action.payload.id;
      const index = state.findIndex((v)=>(v.id===id));
      if (index < 0) {
        console.error("Unable to find id in state, mod",id, state)
        return
      }
      state[index] = {
        id,
        poll: action.payload.poll,
      };
    },
    pollRemoved(state, action){
      const id = action.payload.id;
      const index = state.findIndex((v)=>(v.id===id));
      if (index < 0) {
        console.error("Unable to find id in state, rm",id, state)
        return
      }
      state.filter(item => item.id !== id)
    }
  },
});

export const { pollAdded, pollModified, pollRemoved } = pollsSlice.actions;
export default pollsSlice.reducer;
