import { redirect } from "react-router-dom";
import PubForm, { PubParams } from "./PubForm";
import ProtectedRoute from "../ProtectedRoute";
import { addNewPub, modifyPub } from "../../dbtools/pubs";
import { getUserFacingErrorMessage } from "../../permissions";
import { notifyError } from "../../utils/notify";

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
    pubImage: data.get("pubImage")
  };
  console.log("Pub action data", data, "pubParms", pubParams);
  for (const key of Object.keys(PubParams)) {
    if (key === "food" && pubParams.venueType === "restaurant") {
      pubParams[key] = true;
      continue;
    }

    pubParams[key] = Boolean(data.get(key));
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
