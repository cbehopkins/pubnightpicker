import PubForm from "./PubForm";
import { useRouteLoaderData, defer } from "react-router-dom";
import usePubs from "../../hooks/usePubs";

// FIXME move this and NewPub and Edit Pub somewhere sensible
export async function loader({ request, params }) {
  const pubId = params.pubId;
  return defer({
    pub_id: pubId,
  });
}

function EditPubPage() {
  const pub_parameters = usePubs();
  const { pub_id } = useRouteLoaderData("pub_id");
  const pubObject = pub_parameters[pub_id];
  return <PubForm method="patch" pub_object={pubObject} />;
}

export default EditPubPage;
