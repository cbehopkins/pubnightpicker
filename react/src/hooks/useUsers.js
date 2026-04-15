import { useEffect, useCallback } from "react";
import { collection, onSnapshot } from "firebase/firestore";
import { db } from "../firebase";
import { useSelector, useDispatch } from "react-redux";
import { userAdded, clearUser } from "../store/usersSlice";
import { useAuthState } from "react-firebase-hooks/auth";
import { auth } from "../firebase";

/** @typedef {import("../store").RootState} RootState */

function userSubscription(add_callback, mod_callback, rm_callback) {
  return onSnapshot(collection(db, "user-public"), (snapshot) => {
    snapshot.docChanges().forEach((change) => {
      if (change.type === "added") {
        add_callback(change.doc.id, change.doc.data());
      }
      if (change.type === "modified") {
        mod_callback(change.doc.id, change.doc.data());
      }
      if (change.type === "removed") {
        rm_callback(change.doc.id, change.doc.data());
      }
    });
  });
}

export function useUsersSource() {
  const [user, loading] = useAuthState(auth);
  const dispatch = useDispatch();
  const addCallback = useCallback(
    (id, doc) => {
      const uid = doc?.uid || id;
      dispatch(
        userAdded({
          uid,
          name: doc.name,
          photoUrl: doc.photoUrl,
          votesVisible: doc?.votesVisible !== false,
        })
      );
    },
    [dispatch]
  );

  const modCallback = useCallback(
    (id, doc) => {
      const uid = doc?.uid || id;
      dispatch(
        userAdded({
          uid,
          name: doc.name,
          photoUrl: doc.photoUrl,
          votesVisible: doc?.votesVisible !== false,
        })
      );
    },
    [dispatch]
  );

  const rmCallback = useCallback(
    (id, doc) => {
      const uid = doc?.uid || id;
      dispatch(clearUser({ uid }));
    },
    [dispatch]
  );

  useEffect(() => {
    if (loading || !user) {
      return;
    }
    const unsubscribe = userSubscription(addCallback, modCallback, rmCallback);
    return unsubscribe;
  }, [user, loading, addCallback, modCallback, rmCallback]);
}

function useUsers() {
  return useSelector(
    /** @param {RootState} state */
    (state) => {
      return state.users;
    }
  );
}
export default useUsers;
