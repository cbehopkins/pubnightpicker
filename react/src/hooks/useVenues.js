import { useVenueStreamState } from "./usePubs";

export { normalizeVenue } from "../utils/venueModel";

export function useVenuesState() {
  const { domainVenues, isLoading, isStale, errorMessage, retry } = useVenueStreamState();
  return { venues: domainVenues, isLoading, isStale, errorMessage, retry };
}

export default function useVenues() {
  return useVenuesState().venues;
}