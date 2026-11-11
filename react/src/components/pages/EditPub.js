import PubForm from "./PubForm";
import { useRouteLoaderData, defer } from "react-router-dom";
import { usePubsState } from "../../hooks/usePubs";
import ProtectedRoute from "../ProtectedRoute";

// FIXME move this and NewPub and Edit Pub somewhere sensible
export async function loader({ request, params }) {
  const pubId = params.pubId;
  return defer({
    pub_id: pubId,
  });
}

function EditPubPage() {
  const { venues: pub_parameters, isLoading, isStale, errorMessage } = usePubsState();
  const { pub_id } = useRouteLoaderData("pub_id");
  const pubObject = pub_parameters[pub_id];

  if (!pubObject) {
    if (errorMessage) {
      return <p role="alert">{errorMessage}</p>;
    }
    return isLoading ? <p role="status">Loading venue...</p> : <p role="alert">Venue not found.</p>;
  }

  return (
    <>
      {isStale && (
        <p className="alert alert-warning py-2" role="status">
          Showing the last loaded venue details. Updates may be delayed while the connection recovers.
        </p>
      )}
      <PubForm method="patch" pub_object={pubObject} />
    </>
  );
}

export default ProtectedRoute(EditPubPage, "canManagePubs", "/");
