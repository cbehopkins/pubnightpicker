import { doc, getDoc } from "firebase/firestore";
import { db } from "../firebase";

export default async function getUserDoc(uid, onSuccess = null, onFail = null) {
    const userDoc = await getDoc(doc(db, "users", uid));

    if (!userDoc.exists()) {
        if (onFail === null) {
            console.error("Error with user doc fetch", uid)
            return null
        }
        return onFail(userDoc)
    }
    if (onSuccess === null) {
        return userDoc
    }
    return onSuccess(userDoc)
}
