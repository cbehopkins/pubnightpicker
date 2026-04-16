import { useCallback, useEffect } from "react";
import { useAuthState } from "react-firebase-hooks/auth";
import { auth } from "../firebase";
import { onSnapshot, updateDoc, doc, setDoc } from "firebase/firestore";
import { db } from "../firebase";
import { authAdded, clearAuth } from "../store/authSlice";
import { useDispatch } from "react-redux";
import { notifyError } from "../utils/notify";

async function ensureCanonicalUserDocFromAuth(user) {
    if (!user?.uid) return;

    const authProvider = user.providerData?.[0]?.providerId || "password";

    try {
        // Only ensure canonical identity fields exist here. Preferred display fields
        // are managed in user-public and should not be overwritten by auth defaults.
        await setDoc(doc(db, "users", user.uid), {
            uid: user.uid,
            email: user.email || "",
            authProvider,
        }, { merge: true });
    } catch (err) {
        console.error("Failed to self-heal canonical users doc from auth", err);
    }
}

function selfSubscription(uid, update_callback) {
    const selfDocRef = doc(db, "users", uid);
    return onSnapshot(selfDocRef, (snapshot) => {
        if (!snapshot.exists()) {
            return;
        }
        update_callback(snapshot.data());
    }, (err) => {
        console.error(err);
        notifyError(err.message);
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
    const logInFromAuthUser = useCallback((authUser) => {
        if (!authUser?.uid) {
            return;
        }
        dispatch(authAdded({
            uid: authUser.uid,
            name: authUser.displayName || authUser.email || "",
            email: authUser.email || "",
            photoUrl: authUser.photoURL || null,
        }));
    }, [dispatch]);
    const logInUser = useCallback(async (data) => {
        const resolvedUid = data?.uid || auth.currentUser?.uid;
        if (!resolvedUid) {
            return;
        }

        if (data.authProvider === "google" && !data?.customPhotoUrl) {
            if (data?.photoUrl !== auth.currentUser.photoURL) {
                data.photoUrl = auth.currentUser.photoURL
                // FIXME await this after the dispatch
                await updatePhotoUrl(resolvedUid, data.photoUrl)
            }
        }
        const photoUrl = data?.photoUrl ?? auth.currentUser?.photoURL ?? null
        dispatch(
            authAdded({
                name: data?.name || auth.currentUser?.displayName || auth.currentUser?.email || "",
                uid: resolvedUid,
                email: data?.email || auth.currentUser?.email || "",
                photoUrl,
            })
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

        // Keep auth/uid available for role-gated UI even if users/{uid} hydration is delayed.
        logInFromAuthUser(user);

        // Ensure canonical users/{uid} exists before listening.
        void ensureCanonicalUserDocFromAuth(user);
        const unsubscribe = selfSubscription(user.uid, logInUser);
        return unsubscribe;
    }, [user, loading, logInFromAuthUser, logInUser, logOutUser]);
};
