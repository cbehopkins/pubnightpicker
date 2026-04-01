import { getUserFacingErrorMessage } from "../permissions";
import { notifyError } from "./notify";

/**
 * Generic wrapper for executing async actions with error handling
 * Catches errors and displays user-friendly notifications
 * Used for any async operation, not just attendance (despite the name in attendance.js)
 * 
 * @param {Function} action - Async function to execute
 * @param {string} fallbackMessage - User-friendly error message if action fails
 */
export async function wrapAsyncAction(action, fallbackMessage = "Unable to perform this action.") {
    try {
        await action();
    } catch (error) {
        notifyError(getUserFacingErrorMessage(error, fallbackMessage));
    }
}

// Keep legacy attendance semantics while the generic helper is adopted.
export async function runAttendanceAction(action, fallbackMessage = "Unable to update your attendance.") {
    return wrapAsyncAction(action, fallbackMessage);
}
