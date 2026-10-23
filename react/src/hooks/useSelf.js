import { useCallback, useEffect } from "react";
import { useAuthState } from "react-firebase-hooks/auth";
import { auth } from "../firebase";
import { onSnapshot, doc, setDoc } from "firebase/firestore";
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
            email: user.email || "",
            authProvider,
        }, { merge: true });
    } catch (err) {
        console.error("Failed to self-heal canonical users doc from auth", err);
    }
}

function selfSubscription(uid, updateCallback) {
    let privateData = {};
    let publicData = {};
    let privateLoaded = false;
    let publicLoaded = false;

    const publish = () => {
        if (privateLoaded && publicLoaded) {
            updateCallback(privateData, publicData);
        }
    };
    const handleError = (err) => {
        console.error(err);
        notifyError(err.message);
    };

    const unsubscribePrivate = onSnapshot(doc(db, "users", uid), (snapshot) => {
        privateLoaded = true;
        privateData = snapshot.exists() ? snapshot.data() : {};
        publish();
    }, handleError);
    const unsubscribePublic = onSnapshot(doc(db, "user-public", uid), (snapshot) => {
        publicLoaded = true;
        publicData = snapshot.exists() ? snapshot.data() : {};
        publish();
    }, handleError);

    return () => {
        unsubscribePrivate();
        unsubscribePublic();
    };
}

async function updatePhotoUrl(uid, photoUrl) {
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
    const logInUser = useCallback(async (privateData, loadedPublicData) => {
        const resolvedUid = auth.currentUser?.uid;
        if (!resolvedUid) {
            return;
        }

        let publicData = loadedPublicData;
        const authProvider = privateData?.authProvider
            || auth.currentUser?.providerData?.[0]?.providerId;
        const authPhotoUrl = auth.currentUser?.photoURL || null;
        if (
            (authProvider === "google" || authProvider === "google.com")
            && !publicData?.customPhotoUrl
            && publicData?.photoUrl !== authPhotoUrl
        ) {
            await updatePhotoUrl(resolvedUid, authPhotoUrl);
            publicData = { ...publicData, photoUrl: authPhotoUrl };
        }

        dispatch(
            authAdded({
                name: publicData?.name || auth.currentUser?.displayName || auth.currentUser?.email || "",
                uid: resolvedUid,
                email: auth.currentUser?.email || privateData?.email || "",
                photoUrl: publicData?.customPhotoUrl || publicData?.photoUrl || authPhotoUrl,
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

        // Keep auth/uid available for role-gated UI while profile documents load.
        logInFromAuthUser(user);

        // Ensure private identity data exists before listening for both profiles.
        void ensureCanonicalUserDocFromAuth(user);
        const unsubscribe = selfSubscription(user.uid, logInUser);
        return unsubscribe;
    }, [user, loading, logInFromAuthUser, logInUser, logOutUser]);
};
