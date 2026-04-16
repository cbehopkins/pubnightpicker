import { useCallback, useEffect } from "react";
import { useAuthState } from "react-firebase-hooks/auth";
import { auth } from "../firebase";
import { query, where, collection, onSnapshot, updateDoc, getDocs, doc, setDoc } from "firebase/firestore";
import { db } from "../firebase";
import { authAdded, clearAuth } from "../store/authSlice";
import { useDispatch } from "react-redux";
import { notifyError } from "../utils/notify";

function pickPreferredUserDoc(snapshot, uid) {
    if (snapshot.empty) {
        return null;
    }
    const canonicalDoc = snapshot.docs.find((d) => d.id === uid);
    return canonicalDoc || snapshot.docs[0];
}

async function ensureCanonicalUserDoc(uid, sourceDoc) {
    if (!sourceDoc || sourceDoc.id === uid) {
        return;
    }
    try {
        await setDoc(doc(db, "users", uid), {
            ...sourceDoc.data(),
            uid,
        }, { merge: true });
    } catch (err) {
        console.error("Failed to self-heal canonical users doc", err);
    }
}

function selfSubscription(uid, update_callback, remove_callback) {
    const q = query(collection(db, "users"), where("uid", "==", uid));
    return onSnapshot(q, (snapshot) => {
        if (snapshot.empty) {
            remove_callback();
            return;
        }

        const preferredDoc = pickPreferredUserDoc(snapshot, uid);
        if (!preferredDoc) {
            remove_callback();
            return;
        }

        update_callback(preferredDoc.data());

        // Gradual self-heal: create canonical users/{uid} from legacy auto-id docs.
        void ensureCanonicalUserDoc(uid, preferredDoc);
    }, (err) => {
        console.error(err);
        notifyError(err.message);
    });
}

async function updatePhotoUrl(uid, photoUrl) {
    // Update legacy doc (found by uid field) and canonical doc (by doc ID)
    const q = query(collection(db, "users"), where("uid", "==", uid));
    const docs = await getDocs(q);
    docs.docs.forEach(async (d) => {
        try {
            await updateDoc(d.ref, { photoUrl });
        } catch (err) {
            console.error(err);
            notifyError(err.message);
        }
    });

    try {
        await setDoc(doc(db, "user-public", uid), {
            uid,
            photoUrl,
        }, { merge: true });
    } catch (err) {
        console.error(err);
        notifyError(err.message);
    }
}

export default function useSelf() {
    const [user, loading] = useAuthState(auth);
    const dispatch = useDispatch();
    const logInUser = useCallback(async (data) => {
        if (data.authProvider === "google" && !data?.customPhotoUrl) {
            if (data?.photoUrl !== auth.currentUser.photoURL) {
                data.photoUrl = auth.currentUser.photoURL
                // FIXME await this after the dispatch
                await updatePhotoUrl(data.uid, data.photoUrl)
            }
        }
        const photoUrl = data.photoUrl
        dispatch(
            authAdded({ name: data.name, uid: data.uid, email: data?.email, photoUrl })
        );
    }, [dispatch]);
    const logOutUser = useCallback(() => {
        dispatch(clearAuth());
    }, [dispatch]);

    useEffect(() => {
        if (loading) return;
        if (!user) {
            logOutUser();
            return;
        }
        const unsubscribe = selfSubscription(user.uid, logInUser, logOutUser);
        return unsubscribe;
    }, [user, loading, logInUser, logOutUser]);
};
