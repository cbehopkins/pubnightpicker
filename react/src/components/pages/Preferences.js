import PreferencesForm from "./PreferencesForm";
import { NavLink, redirect, useNavigate } from "react-router-dom";
import { useState, useEffect, useMemo } from "react"
import { useSelector } from "react-redux";
import { store } from "../../store";
import {
  setDoc,
  updateDoc,
  doc as firestoreDoc,
} from "firebase/firestore";
import {
  db,
  reauthenticatePasswordUser,
  requestLoginEmailChange,
} from "../../firebase";
import TextModal from "../UI/TextModal";
import ConfirmModal from "../UI/ConfirmModal";
import Button from "../UI/Button";
import {
  getAuth, updatePassword,
} from "firebase/auth";
import { useAuthState } from "react-firebase-hooks/auth";
import styles from "./Preferences.module.css";
import { notifyError, notifyInfo } from "../../utils/notify";
import { Card, Form } from "react-bootstrap";
import {
  applyThemeMode,
  getStoredThemeMode,
  setStoredThemeMode,
  subscribeToSystemThemeChanges,
} from "../../utils/themeMode";
import { normalizeArrivalTime } from "../../utils/arrivalTime";

function formatRoleName(roleName) {
  return roleName
    .split(/(?=[A-Z])/)
    .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
    .join(" ")
}

function MyRoles() {
  const roles = useSelector((state) => state.auth.roles)

  const rolesList = useMemo(() => {
    if (!roles) return []
    return Object.entries(roles)
      .filter(([, value]) => value === true || (typeof value === "object" && Object.keys(value).length > 0))
      .map(([roleName]) => roleName)
      .sort()
  }, [roles])

  return (
    <div className={styles.rolesSection}>
      <h3>Your Roles</h3>
      {rolesList.length > 0 ? (
        <table className={styles.rolesTable}>
          <thead>
            <tr>
              <th>Role</th>
              <th>Status</th>
            </tr>
          </thead>
          <tbody>
            {rolesList.map((roleName) => (
              <tr key={roleName}>
                <td className={styles.roleName}>{formatRoleName(roleName)}</td>
                <td className={styles.status}>
                  <span className={styles.badge}>✓ Active</span>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : (
        <p className={styles.noRoles}>You don't have any special roles yet.</p>
      )}
    </div>
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
      if (error?.code === "auth/requires-recent-login") {
        setDoReauthenticate(passwordValue)
      } else {
        setErrorString(error?.message || "Could not change password")
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
    const reauthResult = await reauthenticatePasswordUser(passwordValue)
    if (!reauthResult.ok) {
      setErrorString(reauthResult.message)
      return
    }

    try {
      // doReauthenticate contains the new password set in the previous dialog box
      await updatePassword(user, doReauthenticate)
    } catch (error) {
      setErrorString(error?.message || "Could not change password")
      return
    }

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
    <Button type="button" variant="secondary" onClick={() => { setShowPasswordChange(true) }}>
      Change Password
    </Button>
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

function ChangeMyLoginEmail() {
  const [showEmailChange, setShowEmailChange] = useState(false);
  const [showReauthenticate, setShowReauthenticate] = useState(false);
  const [pendingEmail, setPendingEmail] = useState("");
  const [errorString, setErrorString] = useState("")

  const emailChangeHandler = async (event, ref) => {
    event.preventDefault()

    const requestedEmail = String(ref.current.value || "").trim()
    if (!requestedEmail) {
      setErrorString("Cannot have blank email")
      setShowEmailChange(false)
      return
    }

    const changeResult = await requestLoginEmailChange(requestedEmail)
    if (changeResult.ok) {
      setPendingEmail("")
      setShowEmailChange(false)
      setShowReauthenticate(false)
      notifyInfo("Verification email sent. Confirm it to finish updating your login email.")
      return
    }

    if (changeResult.requiresRecentLogin) {
      setPendingEmail(changeResult.email || requestedEmail)
      setShowEmailChange(false)
      setShowReauthenticate(true)
      return
    }

    setErrorString(changeResult.message || "Could not start login email change")
    setShowEmailChange(false)
  }

  const reauthenticateHandler = async (event, ref) => {
    event.preventDefault()
    const passwordValue = String(ref.current.value || "")

    const reauthResult = await reauthenticatePasswordUser(passwordValue)
    if (!reauthResult.ok) {
      setErrorString(reauthResult.message)
      return
    }

    const changeResult = await requestLoginEmailChange(pendingEmail)
    if (changeResult.ok) {
      setPendingEmail("")
      setShowReauthenticate(false)
      notifyInfo("Verification email sent. Confirm it to finish updating your login email.")
      return
    }

    setErrorString(changeResult.message || "Could not start login email change")
  }

  return <>
    {errorString && <ConfirmModal
      title="Error in preferences change"
      detail={errorString}
      confirm_text="Ok"
      on_confirm={() => setErrorString("")}
      confirm_only={true}
    />}
    <Button type="button" variant="secondary" onClick={() => setShowEmailChange(true)}>
      Change Login Email
    </Button>
    {showEmailChange && <TextModal
      title="Change Login Email"
      detail="New login email"
      input_type="email"
      name="login_email"
      confirm_text="Send Verification"
      cancel_text="Abort!"
      on_confirm={emailChangeHandler}
      on_cancel={() => setShowEmailChange(false)}
    />}
    {showReauthenticate && <TextModal
      title="Reauthenticate"
      detail="Reauthentication needed. Please re-enter your current password"
      input_type="password"
      name="password"
      confirm_text="submit"
      cancel_text="Abort!"
      on_confirm={reauthenticateHandler}
      on_cancel={() => {
        setPendingEmail("")
        setShowReauthenticate(false)
      }}
    />}
  </>
}

function Preferences(params) {
  const auth = getAuth();
  const navigate = useNavigate();
  const [user, loading] = useAuthState(auth);
  const [themeMode, setThemeMode] = useState(getStoredThemeMode);

  useEffect(() => {
    if (loading) {
      // maybe trigger a loading screen
      return;
    }
    if (!user) navigate("/");
  }, [user, loading, navigate]);

  useEffect(() => {
    applyThemeMode(themeMode);

    if (themeMode !== "auto") {
      return undefined;
    }

    const handleChange = () => applyThemeMode("auto");
    return subscribeToSystemThemeChanges(handleChange);
  }, [themeMode]);

  const handleThemeModeChange = (event) => {
    const nextMode = event.target.value;
    setThemeMode(nextMode);
    setStoredThemeMode(nextMode);
  };

  // FIXME, this feels hacky!
  const isPassword = !loading && user && auth && (auth.currentUser.providerData[0].providerId === "password")
  return <div className="container py-3 d-flex flex-column gap-3" style={{ minHeight: "100vh" }}>
    <Card>
      <Card.Body>
        <Form.Group controlId="theme_mode">
          <Form.Label className="fw-semibold">Theme</Form.Label>
          <Form.Select value={themeMode} onChange={handleThemeModeChange}>
            <option value="dark">Dark</option>
            <option value="light">Light</option>
            <option value="auto">Auto (match system)</option>
          </Form.Select>
          <Form.Text className="text-body-secondary">
            Default is Auto mode. Auto follows your operating system setting.
          </Form.Text>
        </Form.Group>
      </Card.Body>
    </Card>
    {/* Only allow local password-auth users to modify password/login email */}
    {isPassword && <div className="d-flex flex-wrap gap-2">
      <ChangeMyPassword />
      <ChangeMyLoginEmail />
    </div>}
    <Card>
      <Card.Body>
        <p className="mb-0">
          Read how your data is handled in the <NavLink to="/privacy">Privacy Notice</NavLink>.
        </p>
      </Card.Body>
    </Card>
    <PreferencesForm method="post" />
    <MyRoles />
  </div>
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
    defaultArrivalTime: normalizeArrivalTime(data.get("default_arrival_time")),
    customPhotoUrl,
    photoUrl: defaultAvatar ? photoUrl : avatarUrl,
    pushPreferences: data.get("push_prefs_visible")
      ? {
        pollOpens: Boolean(data.get("push_poll_opens")),
        pollCompletes: Boolean(data.get("push_poll_completes")),
        globalChat: Boolean(data.get("push_global_chat")),
        eventChat: Boolean(data.get("push_event_chat")),
      }
      : undefined,
  };
  if (method === "POST") {
    try {
      // Firestore rejects fields with value `undefined`. Remove any undefined values
      const cleaned = Object.fromEntries(
        Object.entries(notificationParams).filter(([, v]) => v !== undefined)
      );

      // Write private data to users collection
      const privateData = {
        notificationEmail: cleaned.notificationEmail,
        notificationEmailEnabled: cleaned.notificationEmailEnabled,
        openPollEmailEnabled: cleaned.openPollEmailEnabled,
        defaultArrivalTime: cleaned.defaultArrivalTime,
        customPhotoUrl: cleaned.customPhotoUrl,
        pushPreferences: cleaned.pushPreferences,
      };

      // Remove undefined private fields
      const cleanedPrivate = Object.fromEntries(
        Object.entries(privateData).filter(([, v]) => v !== undefined)
      );

      if (Object.keys(cleanedPrivate).length > 0) {
        await updateDoc(firestoreDoc(db, "users", uid), cleanedPrivate);
      }

      // Write public data to user-public collection
      const publicData = {
        uid,
        name: cleaned.name,
        photoUrl: cleaned.photoUrl,
        votesVisible: cleaned.votesVisible,
      };

      // Remove undefined public fields
      const cleanedPublic = Object.fromEntries(
        Object.entries(publicData).filter(([, v]) => v !== undefined)
      );

      if (Object.keys(cleanedPublic).length > 0) {
        await setDoc(firestoreDoc(db, "user-public", uid), cleanedPublic, { merge: true });
      }
    } catch (err) {
      console.error("[Preferences save error]", err?.code, err?.message, err);
      notifyError(`[${err?.code ?? "unknown"}] ${err?.message}`);
    }
  }
  return redirect("/");
}
