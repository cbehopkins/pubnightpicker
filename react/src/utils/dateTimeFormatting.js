function padTwoDigits(value) {
    return String(value).padStart(2, "0");
}

/**
 * Format a Date using local timezone in an ANSI-like sortable form.
 * Example: 2026-05-26 21:04:09
 * @param {Date} dateValue
 * @returns {string | null}
 */
export function formatLocalDateTime(dateValue) {
    if (!(dateValue instanceof Date) || Number.isNaN(dateValue.getTime())) {
        return null;
    }

    const year = dateValue.getFullYear();
    const month = padTwoDigits(dateValue.getMonth() + 1);
    const day = padTwoDigits(dateValue.getDate());
    const hours = padTwoDigits(dateValue.getHours());
    const minutes = padTwoDigits(dateValue.getMinutes());
    const seconds = padTwoDigits(dateValue.getSeconds());

    return `${year}-${month}-${day} ${hours}:${minutes}:${seconds}`;
}