import PubForm from "./PubForm";
import { useRouteLoaderData, defer, useNavigate } from "react-router-dom";
import usePubs from "../../hooks/usePubs";
import useRole from "../../hooks/useRole";
import { useEffect } from "react";

// FIXME move this and NewPub and Edit Pub somewhere sensible
export async function loader({ request, params }) {
  const pubId = params.pubId;
  return defer({
    pub_id: pubId,
  });
}

function EditPubPage() {
  const navigate = useNavigate();
  const canManagePubs = useRole("canManagePubs");

  useEffect(() => {
    if (!canManagePubs) {
      navigate("/");
    }
  }, [canManagePubs, navigate]);

  const pub_parameters = usePubs();
  const { pub_id } = useRouteLoaderData("pub_id");
  const pubObject = pub_parameters[pub_id];

  if (!pubObject) {
    return <p>Loading venue...</p>;
  }

  return <PubForm method="patch" pub_object={pubObject} />;
}

export default EditPubPage;
