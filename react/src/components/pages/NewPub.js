import { redirect } from "react-router-dom";
import PubForm, { PubParams } from "./PubForm";
import ProtectedRoute from "../ProtectedRoute";
import { addNewPub, modifyPub } from "../../dbtools/pubs";
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

function NewPubPage() {
  return <PubForm method="post" />;
}

export default ProtectedRoute(NewPubPage, "canManagePubs", "/");

export async function action({ request, params }) {
  const method = request.method;
  const data = await request.formData();
  const pub_id = params.pubId;
  const pubParams = {
    name: data.get("name"),
    venueType: data.get("venueType") || "pub",
    web_site: data.get("web_site"),
    map: data.get("map"),
    address: data.get("address"),
    notes: data.get("notes"),
    pubImage: data.get("pubImage")
  };
  const recurrence = buildRecurrenceFromFormData(data);
  console.log("Pub action data", data, "pubParms", pubParams);
  for (const key of Object.keys(PubParams)) {
    if (key === "food" && pubParams.venueType === "restaurant") {
      pubParams[key] = true;
      continue;
    }

    pubParams[key] = Boolean(data.get(key));
  }
  if (pubParams.venueType === "event" && recurrence) {
    pubParams.recurrence = recurrence;
  }
  console.log("Submitted parameters", pubParams)
  try {
    if (method === "POST") {
      await addNewPub(pubParams);
    }
    if (method === "PATCH") {
      console.log("Modifying pub", pub_id, "with", pubParams, "data", data);
      await modifyPub(pub_id, pubParams);
    }
  } catch (error) {
    notifyError(getUserFacingErrorMessage(error, "Unable to save this pub."));
    return null;
  }
  return redirect("/venues");
}
