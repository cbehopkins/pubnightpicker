import PubForm from "./PubForm";
import { useRouteLoaderData } from "react-router-dom";
import { usePubsState } from "../../hooks/usePubs";
import ProtectedRoute from "../ProtectedRoute";
import Button from "../UI/Button";

// FIXME move this and NewPub and Edit Pub somewhere sensible
export async function loader({ params }) {
  const pubId = params.pubId;
  return {
    pub_id: pubId,
  };
}

function EditPubPage() {
  const { venues: pub_parameters, isLoading, isStale, errorMessage, retry } = usePubsState();
  const { pub_id } = useRouteLoaderData("pub_id");
  const pubObject = pub_parameters[pub_id];

  if (!pubObject) {
    if (errorMessage) {
      return (
        <div className="alert alert-warning" role="alert">
          <p className="mb-2">{errorMessage}</p>
          <Button type="button" variant="secondary" size="sm" onClick={retry}>Retry</Button>
        </div>
      );
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
