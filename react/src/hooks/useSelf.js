import { useCallback, useEffect } from "react";
import { useAuthState } from "react-firebase-hooks/auth";
import { auth } from "../firebase";
import { onSnapshot, updateDoc, doc, setDoc } from "firebase/firestore";
import { db } from "../firebase";
import { authAdded, clearAuth } from "../store/authSlice";
import { useDispatch } from "react-redux";
import { notifyError } from "../utils/notify";

function selfSubscription(uid, update_callback, remove_callback) {
    const docRef = doc(db, "users", uid);
    return onSnapshot(docRef, (snapshot) => {
        if (snapshot.exists()) {
            update_callback(snapshot.data());
        } else {
            remove_callback();
        }
    });
}

async function updatePhotoUrl(uid, photoUrl) {
    try {
        await updateDoc(doc(db, "users", uid), { photoUrl });
    } catch (err) {
        console.error(err);
        notifyError(err.message);
    }

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
