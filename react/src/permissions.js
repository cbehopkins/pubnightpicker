import { store } from "./store";

export const PERMISSIONS = {
    canChat: "canChat",
    canAddPubToPoll: "canAddPubToPoll",
    canCreatePoll: "canCreatePoll",
    canCompletePoll: "canCompletePoll",
    canManagePubs: "canManagePubs",
    canShowVoters: "canShowVoters",
    canDeleteAnyMessage: "canDeleteAnyMessage",
}

export const CONSOLIDATED_PERMISSION_COLUMNS = [
    { key: PERMISSIONS.canChat, label: "Can Chat" },
    { key: PERMISSIONS.canAddPubToPoll, label: "Can Add/Delete Poll Pubs" },
    { key: PERMISSIONS.canCreatePoll, label: "Can Create/Delete Poll" },
    { key: PERMISSIONS.canCompletePoll, label: "Can Complete/Reschedule" },
    { key: PERMISSIONS.canManagePubs, label: "Can Manage Pubs" },
    { key: PERMISSIONS.canShowVoters, label: "Can Show Voters" },
    { key: PERMISSIONS.canDeleteAnyMessage, label: "Can Delete Any Message" },
]

export const KNOWN_DEFAULT_PERMISSIONS = [
    PERMISSIONS.canChat,
    PERMISSIONS.canAddPubToPoll,
    PERMISSIONS.canShowVoters,
]

export const ADMIN_DEFAULT_PERMISSIONS = [
    PERMISSIONS.canChat,
    PERMISSIONS.canAddPubToPoll,
    PERMISSIONS.canCreatePoll,
    PERMISSIONS.canCompletePoll,
    PERMISSIONS.canManagePubs,
    PERMISSIONS.canShowVoters,
    PERMISSIONS.canDeleteAnyMessage,
]

export function hasPermissionInRoles(roles, roleName, uid) {
    if (!roles || !uid) {
        return false;
    }

    const roleValue = roles[roleName];
    if (roleValue === true) {
        return true;
    }

    if (roleValue && typeof roleValue === "object") {
        return Boolean(roleValue[uid]);
    }

    return false;
}

export function hasCurrentUserPermission(roleName) {
    const state = store.getState();
    return hasPermissionInRoles(state.auth.roles, roleName, state.auth.uid);
}

export class PermissionError extends Error {
    constructor(roleName, actionDescription) {
        super(`Permission denied for ${actionDescription}. Missing role: ${roleName}`);
        this.name = "PermissionError";
        this.roleName = roleName;
        this.actionDescription = actionDescription;
    }
}

export function assertCurrentUserPermission(roleName, actionDescription) {
    if (hasCurrentUserPermission(roleName)) {
        return;
    }

    throw new PermissionError(roleName, actionDescription);
}

export function getUserFacingErrorMessage(error, fallbackMessage = "Something went wrong. Please try again.") {
    if (error instanceof PermissionError) {
        return `You do not have permission for ${error.actionDescription}.`;
    }

    if (error instanceof Error && error.message) {
        return error.message;
    }

    return fallbackMessage;
}
