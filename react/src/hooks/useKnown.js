import { useSelector } from "react-redux";
export default function useKnown() {
    const known = useSelector((state) => state.auth.known);
    return known;
}
