import VenueForm from "./VenueForm";
import { useRouteLoaderData } from "react-router-dom";
import { useVenuesState } from "../../hooks/useVenues";
import ProtectedRoute from "../ProtectedRoute";
import Button from "../UI/Button";

export async function loader({ params }) {
  const venueId = params.venueId;
  return {
    venue_id: venueId,
  };
}

function EditVenuePage() {
  const { venues, isLoading, isStale, errorMessage, retry } = useVenuesState();
  const { venue_id } = useRouteLoaderData("venue_id");
  const venue = venues[venue_id];

  if (!venue) {
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
      <VenueForm method="patch" venueObject={venue} />
    </>
  );
}

export default ProtectedRoute(EditVenuePage, "canManagePubs", "/");
