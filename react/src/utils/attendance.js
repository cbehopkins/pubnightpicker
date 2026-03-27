import { getUserFacingErrorMessage } from "../permissions";
import { notifyError } from "./notify";

export async function runAttendanceAction(action, fallbackMessage = "Unable to update your attendance.") {
    try {
        await action();
    } catch (error) {
        notifyError(getUserFacingErrorMessage(error, fallbackMessage));
    }
}
