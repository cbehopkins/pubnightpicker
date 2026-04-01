// @ts-check

export const ATTENDANCE_GLOBAL_KEY = "any";

/**
 * @typedef {Object} AttendanceEntry
 * @property {string[]=} canCome
 * @property {string[]=} cannotCome
 */

/**
 * @typedef {Record<string, AttendanceEntry | undefined>} AttendanceMap
 */

/**
 * @typedef {Object} EffectiveAttendanceState
 * @property {string[]} canCome
 * @property {string[]} cannotCome
 * @property {boolean} userCanCome
 * @property {boolean} userCannotCome
 * @property {boolean} hasAttendanceData
 */

/** @param {string[]} [values] */
function dedupe(values = []) {
    return [...new Set(values)];
}

/**
 * @param {AttendanceMap | null | undefined} attendance
 * @param {string | null | undefined} venueId
 * @param {string | null | undefined} currentUserId
 * @returns {EffectiveAttendanceState}
 */
export function getEffectiveAttendanceState(attendance, venueId, currentUserId) {
    /** @type {AttendanceMap} */
    const attendanceMap = attendance || {};
    const currentUserIdOrNull = currentUserId || null;
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
        userCanCome: Boolean(currentUserIdOrNull) && canCome.includes(/** @type {string} */(currentUserIdOrNull)),
        userCannotCome: Boolean(currentUserIdOrNull) && cannotCome.includes(/** @type {string} */(currentUserIdOrNull)),
        hasAttendanceData: canCome.length > 0 || cannotCome.length > 0,
    };
}
