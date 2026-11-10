export function londonDate(now = new Date()) {
    const parts = Object.fromEntries(new Intl.DateTimeFormat("en-GB", {
        timeZone: "Europe/London", year: "numeric", month: "2-digit", day: "2-digit",
    }).formatToParts(now).map(({ type, value }) => [type, value]));
    return `${parts.year}-${parts.month}-${parts.day}`;
}

export function shiftDate(isoDate, days) {
    const date = new Date(`${isoDate}T00:00:00Z`);
    date.setUTCDate(date.getUTCDate() + days);
    return date.toISOString().slice(0, 10);
}

export function recurrenceDates(now = new Date()) {
    const today = londonDate(now);
    return { today, monthly: shiftDate(today, 10), edited: shiftDate(today, 12), weekly: shiftDate(today, 1) };
}

export function displayDate(isoDate) {
    const date = new Date(`${isoDate}T00:00:00Z`);
    const day = date.getUTCDate();
    const suffix = day >= 11 && day <= 13 ? "th" : ({ 1: "st", 2: "nd", 3: "rd" }[day % 10] ?? "th");
    const month = new Intl.DateTimeFormat("en-GB", { month: "long", timeZone: "UTC" }).format(date);
    return `${day}${suffix} ${month} ${date.getUTCFullYear()}`;
}