import { useCallback, useEffect } from "react";
import { useAuthState } from "react-firebase-hooks/auth";
import { auth } from "../firebase";
import { query, where, collection, onSnapshot, updateDoc, getDocs } from "firebase/firestore";
import { db } from "../firebase";
import { authAdded, clearAuth } from "../store/authSlice";
import { useDispatch } from "react-redux";

function selfSubscription(uid, update_callback, remove_callback) {
    const q = query(collection(db, "users"), where("uid", "==", uid));
    return onSnapshot(q, (snapshot) => {
        snapshot.docChanges().forEach((change) => {
            if (change.type === "added") {
                update_callback(change.doc.data())
            }
            if (change.type === "modified") {
                update_callback(change.doc.data())
            }
            if (change.type === "removed") {
                remove_callback()
            }
        });
    });
}

async function updatePhotoUrl(uid, photoUrl) {
    const q = query(collection(db, "users"), where("uid", "==", uid));
    const docs = await getDocs(q);
    docs.docs.forEach(async (doc) => {
        try {
            await updateDoc(doc.ref, { photoUrl: photoUrl })
        } catch (err) {
            console.error(err);
            alert(err.message);
        }
    });
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
