import { initializeApp } from "firebase/app";
import {
  GoogleAuthProvider,
  // FacebookAuthProvider,
  getAuth,
  signInWithPopup,
  signInWithEmailAndPassword,
  createUserWithEmailAndPassword,
  sendPasswordResetEmail,
  signOut,
} from "firebase/auth";
import {
  getFirestore,
  query,
  getDocs,
  collection,
  where,
  addDoc,
} from "firebase/firestore";
import { redirect } from "react-router-dom";
import { firebaseConfig } from "./firebase_config";

const app = initializeApp(firebaseConfig);
const auth = getAuth(app);
const db = getFirestore(app);
const googleProvider = new GoogleAuthProvider();
export async function addUserDoc(uid, name, authProvider, email) {
  return addDoc(collection(db, "users"), {
    uid: uid,
    name: name,
    authProvider: authProvider,
    email: email,
  });
}

const signInWithGoogle = async () => {
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
    alert(err.message);
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
  try {
    const res = await signInWithEmailAndPassword(auth, email, password);
    const user = res.user;
    const q = query(collection(db, "users"), where("uid", "==", user.uid));
    const docs = await getDocs(q);
    const name = user?.displayName
    if (docs.docs.length === 0) {
      await addUserDoc(
        user.uid,
        name,
        "local",
        user.email,
      )
    }
  } catch (err) {
    if (err.name === "FirebaseError") {
      if (err.code === "auth/invalid-email") {
        alert("Invalid email address")
        return
      }
      if (err.code === "auth/invalid-login-credentials") {
        alert("Invalid login details (check email and password)")
        return
      }
    }
    console.error(JSON.stringify(err))
    console.error(err);
    alert(err.message);
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
    alert(err.message);
  }
};
const sendPasswordReset = async (email) => {
  try {
    await sendPasswordResetEmail(auth, email);
    alert("Password reset link sent!");
  } catch (err) {
    console.error(err);
    alert(err.message);
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
  logout,
};
