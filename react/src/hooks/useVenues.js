import { useVenueStreamState } from "./venueStream";

export { normalizeVenue } from "../utils/venueModel";

export function useVenuesState() {
  return useVenueStreamState();
}

export default function useVenues() {
  return useVenuesState().venues;
}
