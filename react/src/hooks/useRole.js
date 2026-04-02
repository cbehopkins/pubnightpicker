// @ts-check

import { useSelector } from "react-redux";
import { hasPermissionInRoles } from "../permissions";

/**
 * Generic hook to check if the current user has a specific role.
 * @param {string} roleName - The name of the role to check (e.g., 'admin', 'known', 'moderator')
 * @returns {boolean} - True if the user has the specified role, false otherwise
 */
export default function useRole(roleName) {
    /** @type {string | null | undefined} */
    const uid = useSelector((state) => {
        const typedState = /** @type {{ auth?: { uid?: string | null } }} */ (state);
        return typedState.auth?.uid;
    });
    /** @type {import("../permissions").RolesMap | null | undefined} */
    const roles = useSelector((state) => {
        const typedState = /** @type {{ auth?: { roles?: import("../permissions").RolesMap } }} */ (state);
        return typedState.auth?.roles;
    });
    return hasPermissionInRoles(roles, roleName, uid);
}
