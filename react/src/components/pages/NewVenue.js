import { redirect } from "react-router-dom";
import VenueForm, { VenueParams } from "./VenueForm";
import ProtectedRoute from "../ProtectedRoute";
import { addNewVenue, modifyVenue } from "../../dbtools/venues";
import { getUserFacingErrorMessage } from "../../permissions";
import { notifyError } from "../../utils/notify";

function buildRecurrenceFromFormData(data) {
  const frequency = data.get("recurrence_frequency") || "none";
  if (frequency === "none") {
    return undefined;
  }

  const recurrence = {
    frequency,
  };

  if (frequency === "once") {
    const date = data.get("recurrence_date");
    if (date) recurrence.date = date;
    return recurrence;
  }

  // For weekly, monthly, yearly: add interval if present
  const interval = data.get("recurrence_interval");
  if (interval) recurrence.interval = Number(interval);

  if (frequency === "weekly") {
    const weekday = data.get("recurrence_weekday");
    if (weekday !== null) recurrence.weekday = Number(weekday);
    return recurrence;
  }

  if (frequency === "monthly") {
    // Include whichever fields were submitted (either fixed day or weekday pattern)
    const monthDay = data.get("recurrence_month_day");
    const weekday = data.get("recurrence_weekday");
    const nth = data.get("recurrence_nth");

    if (monthDay !== null) recurrence.month_day = Number(monthDay);
    if (weekday !== null) recurrence.weekday = Number(weekday);
    if (nth !== null) recurrence.nth = Number(nth);
    return recurrence;
  }

  if (frequency === "yearly") {
    // Include whichever fields were submitted (either fixed date or weekday pattern)
    const month = data.get("recurrence_month");
    const monthDay = data.get("recurrence_month_day");
    const weekday = data.get("recurrence_weekday");
    const nth = data.get("recurrence_nth");

    if (month !== null) recurrence.month = Number(month);
    if (monthDay !== null) recurrence.month_day = Number(monthDay);
    if (weekday !== null) recurrence.weekday = Number(weekday);
    if (nth !== null) recurrence.nth = Number(nth);
    return recurrence;
  }

  return undefined;
}

function NewVenuePage() {
  return <VenueForm method="post" />;
}

export default ProtectedRoute(NewVenuePage, "canManagePubs", "/");

function parseFormObject(data, fieldName) {
  try {
    const value = JSON.parse(data.get(fieldName) || "{}");
    return value && typeof value === "object" && !Array.isArray(value) ? value : {};
  } catch {
    return {};
  }
}

export async function action({ request, params }) {
  const method = request.method;
  const data = await request.formData();
  const venueId = params.venueId;
  const venueParams = {
    name: data.get("name"),
    venueType: data.get("venueType") || "pub",
    website: data.get("website"),
    map: data.get("map"),
    address: data.get("address"),
    notes: data.get("notes"),
    image: data.get("image")
  };
  const recurrence = buildRecurrenceFromFormData(data);
  for (const key of Object.keys(VenueParams)) {
    if (key === "food" && venueParams.venueType === "restaurant") {
      venueParams[key] = true;
      continue;
    }

    venueParams[key] = Boolean(data.get(key));
  }
  venueParams.recurrence = venueParams.venueType === "event" ? recurrence ?? null : null;
  try {
    if (method === "POST") {
      await addNewVenue(venueParams);
    }
    if (method === "PATCH") {
      const baseline = parseFormObject(data, "venue_baseline");
      const editTokens = parseFormObject(data, "edit_tokens");
      await modifyVenue(venueId, venueParams, baseline, editTokens);
      return redirect("/venues");
    }
  } catch (error) {
    if (error?.code === "field_conflict") {
      return {
        conflict: {
          message: error.message,
          fields: error.fields || [],
          currentVenue: error.currentVenue,
        },
      };
    }
    if (error?.code === "invalid_edit_token" || error?.code === "precondition_required") {
      return { saveError: error.message };
    }
    notifyError(getUserFacingErrorMessage(error, "Unable to save this venue."));
    return null;
  }
  return redirect("/venues");
}
