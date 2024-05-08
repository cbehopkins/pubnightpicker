import { useEffect, useCallback } from "react";
import { collection, onSnapshot } from "firebase/firestore";
import { db } from "../firebase";
import { useSelector, useDispatch } from "react-redux";
import { userAdded, clearUser } from "../store/usersSlice";

function userSubscription(add_callback, mod_callback, rm_callback) {
  return onSnapshot(collection(db, "users"), (snapshot) => {
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
  const dispatch = useDispatch();
  const addCallback = useCallback(
    (id, doc) => {
      const votes_visible = doc?.votesVisible;
      dispatch(
        userAdded({
          uid: doc.uid,
          name: doc.name,
          email: doc.email,
          votesVisible: votes_visible,
          photoUrl: doc.photoUrl,
        })
      );
    },
    [dispatch]
  );

  const modCallback = useCallback(
    (id, doc) => {
      const votes_visible = doc?.votesVisible;

      dispatch(
        userAdded({
          uid: doc.uid,
          name: doc.name,
          email: doc.email,
          votesVisible: votes_visible,
          photoUrl: doc.photoUrl,
        })
      );
    },
    [dispatch]
  );

  const rmCallback = useCallback(
    (id, doc) => {
      dispatch(clearUser({ uid: doc.uid }));
    },
    [dispatch]
  );

  useEffect(() => {
    const unsubscribe = userSubscription(addCallback, modCallback, rmCallback);
    return unsubscribe;
  }, [addCallback, modCallback, rmCallback]);
}

function useUsers() {
  return useSelector((state) => state.users);
}
export default useUsers;
