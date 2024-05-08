import PreferencesForm from "./PreferencesForm";
import { redirect, useNavigate } from "react-router-dom";
import { useState, useEffect } from "react"
import { store } from "../../store";
import {
  updateDoc,
} from "firebase/firestore";
import TextModal from "../UI/TextModal";
import ConfirmModal from "../UI/ConfirmModal";
import {
  EmailAuthProvider, getAuth, updatePassword, reauthenticateWithCredential,
} from "firebase/auth";
import { useAuthState } from "react-firebase-hooks/auth";
import getUserDoc from "../../dbtools/getUserDoc";

async function ReauthenticateUser(auth, userProvidedPassword) {
  const credential = EmailAuthProvider.credential(
    auth.currentUser.email,
    userProvidedPassword
  )
  return reauthenticateWithCredential(
    auth.currentUser,
    credential
  )
}

function ChangeMyPassword() {
  const [showPasswordChange, setShowPasswordChange] = useState(false);
  const [doReauthenticate, setDoReauthenticate] = useState(""); // Preserve the attempted new password here until we have reauthenticated
  const [errorString, setErrorString] = useState("")
  const auth = getAuth();
  const user = auth.currentUser;
  const passwordChangeHandler = async (event, ref) => {
    event.preventDefault()
    const passwordValue = ref.current.value
    if (!passwordValue) {
      setErrorString("Cannot have blank password")
      setShowPasswordChange(false)
      return
    }
    // Change the password here
    try {
      await updatePassword(user, passwordValue)
    } catch (error) {
      if (error.message.includes("auth/requires-recent-login")) {
        setDoReauthenticate(passwordValue)
      } else {
        setErrorString(error.message)
      }
    }
    setShowPasswordChange(false)
  }
  const reauthenticateHandler = async (event, ref) => {
    event.preventDefault()
    const passwordValue = ref.current.value
    if (!passwordValue) {
      setErrorString("Cannot have blank password")
      setShowPasswordChange(false)
      return
    }
    // passwordValue is the old password
    await ReauthenticateUser(auth, passwordValue)
    // doReauthenticate contains the new password set in the previous dialog box
    await updatePassword(user, doReauthenticate)
    setDoReauthenticate("")
  }
  return <>
    {errorString && <ConfirmModal
      title="Error in preferences change"
      detail={errorString}
      confirm_text="Ok"
      on_confirm={() => setErrorString("")}
      confirm_only={true}
    />}
    <button onClick={() => { setShowPasswordChange(true) }}>Change Password</button>
    {showPasswordChange && <TextModal
      title="Change Password"
      detail="New Password"
      input_type="password"
      name="password"
      confirm_text="Change It"
      cancel_text="Abort!"
      on_confirm={passwordChangeHandler}
      on_cancel={() => { setShowPasswordChange(false) }}
    />}
    {doReauthenticate && <TextModal
      title="Reauthenticate"
      detail="Reauthentication needed. Please re-enter your old password"
      input_type="password"
      name="password"
      confirm_text="submit"
      cancel_text="Abort!"
      on_confirm={reauthenticateHandler}
      on_cancel={() => { setDoReauthenticate("") }}
    />}
  </>
}

function Preferences(params) {
  const auth = getAuth();
  const navigate = useNavigate();
  const [user, loading] = useAuthState(auth);
  useEffect(() => {
    if (loading) {
      // maybe trigger a loading screen
      return;
    }
    if (!user) navigate("/");
  }, [user, loading, navigate]);
  // FIXME, this feels hacky!
  const isPassword = !loading && user && auth && (auth.currentUser.providerData[0].providerId === "password")
  return <>
    {/* Only allow change of password here, if it is a local password*/}
    {isPassword && <ChangeMyPassword />}
    <PreferencesForm method="post" />
  </>
}

export default Preferences;



export async function action({ request, params }) {
  const authObj = store.getState()?.auth;
  if (!authObj || !authObj?.loggedIn) {
    console.error("Not logged in, can't update notification settings")
    return redirect("/");
  }
  const uid = authObj.uid;

  const method = request.method;
  const data = await request.formData();
  const doc = await getUserDoc(uid);

  if (!doc) {
    console.error("Error accessing user document");
    return;
  }
  const avatarUrl = data.get("avatar")
  const photoUrl = authObj.photoUrl
  const defaultAvatar = avatarUrl === "" || avatarUrl === photoUrl
  const customPhotoUrl = !defaultAvatar

  const notificationParams = {
    name: data.get("name"),
    notificationEmail: data.get("email"),
    notificationEmailEnabled: Boolean(data.get("emailme")),
    votesVisible: Boolean(data.get("votes_visible")),
    openPollEmailEnabled: Boolean(data.get("open_poll_email")),
    customPhotoUrl,
    photoUrl: defaultAvatar ? photoUrl : avatarUrl,
  };
  if (method === "POST") {
    try {
      updateDoc(doc.ref, notificationParams);
    } catch (err) {
      console.error(err);
      alert(err.message);
    }
  }
  return redirect("/");
}
