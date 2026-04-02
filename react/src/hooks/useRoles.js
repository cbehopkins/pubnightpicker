// @ts-check

import { useCallback, useMemo } from "react";
import { query, collection } from "firebase/firestore";
import { db } from "../firebase";
import useQueryDb from "./useQueryDb";

/** @typedef {Record<string, boolean>} RoleUserMap */
/** @typedef {Record<string, RoleUserMap>} RolesMap */

/** @param {{ code?: string, message?: string }} error */
function defaultErrorHandler(error) {
  // we expect error code to be FirestoreErrorCode
  console.log("My Lovely error catcher", error?.code, error.message)
}

/**
 * @param {((error: { code?: string, message?: string }) => void) | null} [error_handler]
 * @returns {RolesMap}
 */
export function useAllRoles(error_handler = null) {
  const errorHandler = error_handler || defaultErrorHandler;
  const q = query(collection(db, "roles"));
  /** @type {RolesMap} */
  const roles = useQueryDb(q, errorHandler)
  return roles;
}

/**
 * @param {{ uid?: string | null } | null | undefined} user
 * @param {boolean} loading
 * @param {((error: { code?: string, message?: string }) => void) | null} [error_handler]
 * @returns {Record<string, boolean>}
 */
function useRoles(user, loading, error_handler = null) {
  /** @type {Record<string, boolean>} */
  const emptyRoles = useMemo(() => ({}), []);
  const userId = !loading && user && user.uid;
  const errorHandler = error_handler || defaultErrorHandler;
  /** @type {(role: RoleUserMap) => boolean} */
  const precondition = useCallback((role) => {
    if (!userId) {
      return false;
    }
    return userId in role
  }, [userId]);

  const q = useMemo(() => query(collection(db, "roles")), []);
  const roles = /** @type {Record<string, boolean>} */ (useQueryDb(q, errorHandler, precondition, Boolean(userId)));
  if (loading || !userId) {
    return emptyRoles;
  }
  return roles;
}
export default useRoles;
