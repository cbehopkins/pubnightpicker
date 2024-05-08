import { useState, useEffect, useCallback } from "react";
import { collection, onSnapshot } from "firebase/firestore";
import { db } from "../firebase";

function pubSubscription(add_callback, mod_callback, rm_callback) {
  return onSnapshot(collection(db, "pubs"), (snapshot) => {
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
function usePubs() {
  const [pubs, setPubs] = useState({});
  const addPubCallback = useCallback((id, pub) => {
    setPubs((prevPubs) => {
      return { ...prevPubs, [id]: pub };
    });
  }, []);
  const modPubCallback = useCallback((id, pub) => {
    setPubs((prevPubs) => {
      const nextPubs = { ...prevPubs };
      nextPubs[id] = pub;
      return nextPubs;
    });
  }, []);
  const rmPubCallback = useCallback((id) => {
    setPubs((prevPubs) => {
      const nextPubs = { ...prevPubs };
      delete nextPubs[id];
      return nextPubs;
    });
  }, []);
  useEffect(() => {
    const unsubscribe = pubSubscription(
      addPubCallback,
      modPubCallback,
      rmPubCallback
    );
    return () => {
      unsubscribe();
    };
  }, [addPubCallback, modPubCallback, rmPubCallback]);
  return pubs;
}
export default usePubs;
