import { useSelector } from "react-redux";

/**
 * Generic hook to check if the current user has a specific role.
 * @param {string} roleName - The name of the role to check (e.g., 'admin', 'known', 'moderator')
 * @returns {boolean} - True if the user has the specified role, false otherwise
 */
export default function useRole(roleName) {
    const uid = useSelector((state) => state.auth.uid);
    const roles = useSelector((state) => state.auth.roles);

    // The roles object from useRoles hook is filtered to only include roles the user has
    // Structure: { admin: true, known: true } or { admin: { userId: true }, ... }
    // We need to handle both cases for compatibility

    if (!roles || !roles[roleName]) {
        return false;
    }

    // If it's a boolean true, user has this role
    if (roles[roleName] === true) {
        return true;
    }

    // If it's an object, check if user's ID is in it (for useAllRoles case)
    if (typeof roles[roleName] === 'object') {
        return Boolean(uid && uid in roles[roleName] && roles[roleName][uid]);
    }

    return false;
}
