export const ATTENDANCE_GLOBAL_KEY = "any";

function dedupe(values = []) {
    return [...new Set(values)];
}

export function getEffectiveAttendanceState(attendance, venueId, currentUserId) {
    const attendanceMap = attendance || {};
    const localAttendance = venueId ? (attendanceMap[venueId] || {}) : {};
    const globalAttendance = attendanceMap[ATTENDANCE_GLOBAL_KEY] || {};

    const localCanCome = localAttendance.canCome || [];
    const localCannotCome = localAttendance.cannotCome || [];

    // The global sentinel should only apply to non-global rows.
    const useGlobalFallback = Boolean(venueId) && venueId !== ATTENDANCE_GLOBAL_KEY;
    const globalCanCome = useGlobalFallback ? (globalAttendance.canCome || []) : [];
    const globalCannotCome = useGlobalFallback ? (globalAttendance.cannotCome || []) : [];

    // Local explicit statuses override the opposite global sentinel status.
    const canCome = dedupe([...globalCanCome, ...localCanCome]).filter((userId) => !localCannotCome.includes(userId));
    const cannotCome = dedupe([...globalCannotCome, ...localCannotCome]).filter((userId) => !localCanCome.includes(userId));

    return {
        canCome,
        cannotCome,
        userCanCome: Boolean(currentUserId) && canCome.includes(currentUserId),
        userCannotCome: Boolean(currentUserId) && cannotCome.includes(currentUserId),
        hasAttendanceData: canCome.length > 0 || cannotCome.length > 0,
    };
}
