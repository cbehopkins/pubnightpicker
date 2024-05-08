import { redirect } from "react-router-dom";
import { db } from "../../firebase";
import { collection, addDoc, doc, updateDoc } from "firebase/firestore";
import PubForm, { PubParams } from "./PubForm";

function NewPubPage() {
  return <PubForm method="post"/>;
}

export default NewPubPage;

const addNewPub = async (pubParams) => {
  try {
    await addDoc(collection(db, "pubs"), pubParams);
  } catch (err) {
    console.error("Error adding document: ", err);
  }
};

const modifyPub = async (id, pubParams) => {
  try {
    const docRef = doc(db, "pubs", id);
    await updateDoc(docRef, pubParams);
  } catch (err) {
    console.error("Error modifying document: ", err);
  }
};

export async function action({ request, params }) {
  const method = request.method;
  const data = await request.formData();
  const pub_id = params.pubId;
  const pubParams = {
    name: data.get("name"),
    web_site: data.get("web_site"),
    map: data.get("map"),
    address: data.get("address"),
    pubImage: data.get("pubImage")
  };
  for (const key of Object.keys(PubParams)) {
    pubParams[key] = Boolean(data.get(key));
  }
  if (method === "POST") {
    // If one awaits these (as one should)
    // The the redirect doesn't work
    addNewPub(pubParams);
  }
  if (method === "PATCH") {
    modifyPub(pub_id, pubParams);
  }
  return redirect("/pubs");
}
