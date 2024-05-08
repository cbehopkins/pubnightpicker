import { query, getDocs, collection, where } from "firebase/firestore";
import { db } from "../firebase";

export default async function getUserDoc(uid, onSuccess = null, onFail = null) {
    const q = query(collection(db, "users"), where("uid", "==", uid));
    const docs = await getDocs(q);

    if (docs.length === 0 || docs.docs.length !== 1) {
        if (onFail === null) {
            console.error("Error with user doc fetch", docs.docs.length, docs)
            return null
        }
        return onFail(docs)
    }
    if (onSuccess === null) {
        return docs.docs[0]
    }
    return onSuccess(docs.docs[0])
}