import useRole from "./useRole";

export default function useAdmin() {
  return useRole("admin");
}
