import { useSelector } from "react-redux";
export default function useAdmin() {
  const admin = useSelector((state) => state.auth.admin);
  return admin;
}
