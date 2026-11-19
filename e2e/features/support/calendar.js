export function londonDate(now = new Date()) {
    const parts = Object.fromEntries(new Intl.DateTimeFormat("en-GB", {
        timeZone: "Europe/London", year: "numeric", month: "2-digit", day: "2-digit",
    }).formatToParts(now).map(({ type, value }) => [type, value]));
    return `${parts.year}-${parts.month}-${parts.day}`;
}

export function londonInstant(isoDate, localTime) {
    const reference = new Date(`${isoDate}T12:00:00Z`);
    const zone = new Intl.DateTimeFormat("en-GB", {
        timeZone: "Europe/London",
        timeZoneName: "longOffset",
    }).formatToParts(reference).find(({ type }) => type === "timeZoneName").value;
    const offset = zone === "GMT" ? "+00:00" : zone.slice(3);
    return `${isoDate}T${localTime}:00${offset}`;
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
