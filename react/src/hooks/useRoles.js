import { useCallback, useMemo } from "react";
import { query, collection } from "firebase/firestore";
import { db } from "../firebase";
import useQueryDb from "./useQueryDb";
function defaultErrorHandler(error) {
  // we expect error code to be FirestoreErrorCode
  console.log("My Lovely error catcher", error?.code, error.message)
}
export function useAllRoles(error_handler = null) {
  const errorHandler = error_handler || defaultErrorHandler;
  const q = query(collection(db, "roles"));
  const roles = useQueryDb(q, errorHandler)
  return roles;
}

function useRoles(user, loading, error_handler = null) {
  const userId = !loading && user && user.uid;
  const errorHandler = error_handler || defaultErrorHandler;
  const precondition = useCallback((role) => {
    return userId in role
  }, [userId]);

  const q = useMemo(() => query(collection(db, "roles")), []);
  var roles = {}
  roles = useQueryDb(q, errorHandler, precondition);
  return roles;
}
export default useRoles;
