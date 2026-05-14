import { initializeApp } from "firebase/app";
import {
  connectAuthEmulator,
  GoogleAuthProvider,
  // FacebookAuthProvider,
  EmailAuthProvider,
  getAuth,
  signInWithPopup,
  signInWithEmailAndPassword,
  fetchSignInMethodsForEmail,
  createUserWithEmailAndPassword,
  reauthenticateWithCredential,
  sendPasswordResetEmail,
  signOut,
  verifyBeforeUpdateEmail,
  deleteUser,
} from "firebase/auth";
import {
  connectFirestoreEmulator,
  initializeFirestore,
  persistentLocalCache,
  persistentMultipleTabManager,
  doc,
  getDoc,
  setDoc,
  query,
  getDocs,
  collection,
  where,
  addDoc,
} from "firebase/firestore";
import { redirect } from "react-router-dom";
import { firebaseConfig } from "./firebase_config";
import { notifyError, notifyInfo } from "./utils/notify";

const app = initializeApp(firebaseConfig);
const auth = getAuth(app);
// persistentLocalCache enables IndexedDB-backed offline caching. Firestore
// will serve previously-read documents while offline and queue writes (votes,
// attendance etc.) for replay when connectivity returns. Falls back to
// memory-only cache in environments that block IndexedDB (e.g. private
// browsing on some browsers).
// persistentMultipleTabManager allows multiple browser tabs to share the same
// IndexedDB cache without fighting for exclusive access.
const db = initializeFirestore(app, {
  localCache: persistentLocalCache({
    tabManager: persistentMultipleTabManager(),
  }),
});
const isLocalDevHost = ["localhost", "127.0.0.1", "[::1]"].includes(
  window.location.hostname,
);
const useFirebaseEmulators =
  isLocalDevHost && import.meta.env.VITE_USE_FIREBASE_EMULATORS !== "false";

if (useFirebaseEmulators) {
  connectAuthEmulator(auth, "http://127.0.0.1:9099", { disableWarnings: true });
  connectFirestoreEmulator(db, "127.0.0.1", 8080);
}

const googleProvider = new GoogleAuthProvider();

function normalizeEmail(value) {
  return String(value || "").trim().toLowerCase();
}

function mapAuthErrorMessage(err, fallbackMessage = "Unable to complete the request") {
  const code = err?.code;

  if (code === "auth/invalid-email") {
    return "Invalid email address";
  }
  if (code === "auth/email-already-in-use") {
    return "Another account already uses that email address";
  }
  if (code === "auth/requires-recent-login") {
    return "Please re-enter your password to continue";
  }
  if (code === "auth/wrong-password") {
    return "Incorrect password";
  }
  if (code === "auth/user-mismatch" || code === "auth/invalid-credential") {
    return "Your login session could not be validated. Please sign in again.";
  }
  if (typeof err?.message === "string" && err.message.trim().length > 0) {
    return err.message;
  }

  return fallbackMessage;
}

/**
 * Write user public profile data to user-public collection
 */
async function addUserPublicProfile(uid, name, photoUrl) {
  return setDoc(doc(db, "user-public", uid), {
    uid: uid,
    name: name,
    photoUrl: photoUrl || null,
    votesVisible: true,
  }, {
    merge: true,
  });
}

/**
 * Write user private data to users collection and public profile to user-public
 */
export async function addUserDoc(uid, name, authProvider, email) {
  // Write private user data
  await setDoc(doc(db, "users", uid), {
    uid: uid,
    name: name,
    authProvider: authProvider,
    email: email,
  }, {
    merge: true,
  });

  // Write public profile data
  await addUserPublicProfile(uid, name, null);
}

const signInWithGoogle = async () => {
  if (useFirebaseEmulators) {
    notifyError(
      "Google sign-in is disabled while Firebase Auth emulator is enabled. Use email/password locally, or set VITE_USE_FIREBASE_EMULATORS=false to test against your Firebase project.",
    );
    return;
  }

  try {
    const res = await signInWithPopup(auth, googleProvider);
    const user = res.user;
    const q = query(collection(db, "users"), where("uid", "==", user.uid));
    const docs = await getDocs(q);
    if (docs.docs.length === 0) {
      await addUserDoc(
        user.uid,
        user.displayName,
        "google",
        user.email,
      )
    }
  } catch (err) {
    console.error("code", err.code); // == resource-exhausted
    console.error(err);
    notifyError(err.message);
  }
};

// const facebookProvider = new FacebookAuthProvider();
// const signInWithFacebook = async () => {
//   try {
//     const res = await signInWithPopup(auth, facebookProvider)
//       // This gives you a Facebook Access Token. You can use it to access the Facebook API.
//     const credential = FacebookAuthProvider.credentialFromResult(res);
//     const accessToken = credential.accessToken;
//     const user = res.user;
//     const q = query(collection(db, "users"), where("uid", "==", user.uid));
//     const docs = await getDocs(q);
//     if (docs.docs.length === 0) {
//       await addDoc(collection(db, "users"), {
//         uid: user.uid,
//         name: user.displayName,
//         authProvider: "facebook",
//         email: user.email,
//       });
//     }

//   } catch (error) {
//     // Handle Errors here.
//     const errorCode = error.code;
//     const errorMessage = error.message;
//     // The email of the user's account used.
//     const email = error.customData.email;
//     // The AuthCredential type that was used.
//     const credential = FacebookAuthProvider.credentialFromError(error);
//     console.error(error);
//     alert(error.message);
//     // ...
//   }
// };
const logInWithEmailAndPassword = async (email, password) => {
  const normalizedEmail = normalizeEmail(email);
  const normalizedPassword = String(password || "");

  if (!normalizedEmail || !normalizedPassword) {
    notifyError("Please enter your email and password");
    return;
  }

  try {
    const res = await signInWithEmailAndPassword(auth, normalizedEmail, normalizedPassword);
    const user = res.user;

    // Keep post-login users/{uid} hydration best-effort so auth success is never masked.
    try {
      const userDoc = await getDoc(doc(db, "users", user.uid));
      if (!userDoc.exists()) {
        await addUserDoc(
          user.uid,
          user?.displayName || user?.email || "",
          "local",
          user.email,
        );
      }
    } catch (profileErr) {
      console.error("Signed in, but could not sync users doc", profileErr);
    }
  } catch (err) {
    if (err.name === "FirebaseError") {
      if (err.code === "auth/user-not-found") {
        notifyError("No account exists for that email")
        return
      }
      if (err.code === "auth/wrong-password") {
        notifyError("Incorrect password")
        return
      }
      if (err.code === "auth/invalid-email") {
        notifyError("Invalid email address")
        return
      }
      if (
        err.code === "auth/invalid-login-credentials"
        || err.code === "auth/invalid-credential"
      ) {
        try {
          const methods = await fetchSignInMethodsForEmail(auth, normalizedEmail);
          if (!methods.length) {
            notifyError("No account exists for that email")
            return
          }
          if (!methods.includes("password")) {
            if (methods.includes("google.com")) {
              notifyError("This account uses Google sign-in. Use Google login or reset password after linking email/password.")
              return
            }
            notifyError(`This account does not support password sign-in (providers: ${methods.join(", ")})`)
            return
          }
        } catch (methodErr) {
          console.error("Failed to fetch sign-in methods", methodErr);
        }

        notifyError("Invalid login details (check email and password)")
        return
      }
    }
    console.error(JSON.stringify(err))
    console.error(err);
    notifyError(err.message);
  }
};

const registerWithEmailAndPassword = async (name, email, password) => {
  try {
    const res = await createUserWithEmailAndPassword(auth, email, password);
    const user = res.user;
    // await addDoc(collection(db, "users"), {
    //   uid: user.uid,
    //   name,
    //   authProvider: "local",
    //   email,
    // });
    await addUserDoc(
      user.uid,
      name,
      "local",
      email,
    )
  } catch (err) {
    console.error(err);
    notifyError(err.message);
  }
};
const sendPasswordReset = async (email) => {
  try {
    await sendPasswordResetEmail(auth, email);
    notifyInfo("Password reset link sent!");
  } catch (err) {
    console.error(err);
    notifyError(err.message);
  }
};

const reauthenticatePasswordUser = async (password) => {
  const currentUser = auth.currentUser;
  if (!currentUser?.uid) {
    return {
      ok: false,
      code: "auth/no-current-user",
      message: "No active user session found",
    };
  }

  const currentPassword = String(password || "");
  if (!currentPassword) {
    return {
      ok: false,
      code: "validation/blank-password",
      message: "Cannot have blank password",
    };
  }

  const currentEmail = normalizeEmail(currentUser.email);
  if (!currentEmail) {
    return {
      ok: false,
      code: "auth/missing-email",
      message: "Your account does not have a password login email",
    };
  }

  try {
    const credential = EmailAuthProvider.credential(currentEmail, currentPassword);
    await reauthenticateWithCredential(currentUser, credential);
    return { ok: true };
  } catch (err) {
    return {
      ok: false,
      code: err?.code || "auth/reauth-failed",
      message: mapAuthErrorMessage(err, "Could not verify your password"),
    };
  }
};

const requestLoginEmailChange = async (nextEmail) => {
  const currentUser = auth.currentUser;
  if (!currentUser?.uid) {
    return {
      ok: false,
      code: "auth/no-current-user",
      message: "No active user session found",
    };
  }

  const normalizedNextEmail = normalizeEmail(nextEmail);
  if (!normalizedNextEmail) {
    return {
      ok: false,
      code: "validation/blank-email",
      message: "Cannot have blank email",
    };
  }

  try {
    await verifyBeforeUpdateEmail(currentUser, normalizedNextEmail);
    return {
      ok: true,
      email: normalizedNextEmail,
    };
  } catch (err) {
    if (err?.code === "auth/requires-recent-login") {
      return {
        ok: false,
        code: err.code,
        requiresRecentLogin: true,
        email: normalizedNextEmail,
        message: mapAuthErrorMessage(err),
      };
    }

    return {
      ok: false,
      code: err?.code || "auth/email-change-failed",
      message: mapAuthErrorMessage(err, "Could not start email change verification"),
    };
  }
};

const deleteCurrentAuthUser = async () => {
  const currentUser = auth.currentUser;
  if (!currentUser?.uid) {
    return {
      ok: false,
      code: "auth/no-current-user",
      message: "No active user session found",
    };
  }

  try {
    await deleteUser(currentUser);
    return { ok: true };
  } catch (err) {
    return {
      ok: false,
      code: err?.code || "auth/delete-failed",
      requiresRecentLogin: err?.code === "auth/requires-recent-login",
      message: mapAuthErrorMessage(err, "Unable to delete this account right now"),
    };
  }
};

const logout = () => {
  signOut(auth);
  redirect("/")
};

export {
  auth,
  db,
  signInWithGoogle,
  logInWithEmailAndPassword,
  registerWithEmailAndPassword,
  sendPasswordReset,
  reauthenticatePasswordUser,
  requestLoginEmailChange,
  deleteCurrentAuthUser,
  logout,
  addUserPublicProfile,
};
