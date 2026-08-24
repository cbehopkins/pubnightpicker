import { Form as RouterForm, useNavigate, useNavigation } from "react-router-dom";
import { doc as firestoreDoc, getDoc, setDoc, updateDoc } from "firebase/firestore";
import { db } from "../../firebase";
import { useSelector } from "react-redux";
import { useEffect, useState } from "react";
import { Card, Col, Form, Row } from "react-bootstrap";
import Button from "../UI/Button";
import useWebPushSettings from "../../hooks/useWebPushSettings";
import { normalizeArrivalTime } from "../../utils/arrivalTime";
import { notifyError } from "../../utils/notify";

// Shared by the self-service preferences route action and the admin "edit for another user" modal.
export async function savePreferences({ uid, formData, currentPhotoUrl }) {
  const avatarUrl = formData.get("avatar");
  const defaultAvatar = avatarUrl === "" || avatarUrl === currentPhotoUrl;

  const privateData = {
    notificationEmail: formData.get("email"),
    notificationEmailEnabled: Boolean(formData.get("emailme")),
    openPollEmailEnabled: Boolean(formData.get("open_poll_email")),
    defaultArrivalTime: normalizeArrivalTime(formData.get("default_arrival_time")),
    customPhotoUrl: !defaultAvatar,
    pushPreferences: formData.get("push_prefs_visible")
      ? {
        pollOpens: Boolean(formData.get("push_poll_opens")),
        pollCompletes: Boolean(formData.get("push_poll_completes")),
        globalChat: Boolean(formData.get("push_global_chat")),
        eventChat: Boolean(formData.get("push_event_chat")),
      }
      : undefined,
  };

  const publicData = {
    uid,
    name: formData.get("name"),
    photoUrl: defaultAvatar ? currentPhotoUrl : avatarUrl,
    votesVisible: Boolean(formData.get("votes_visible")),
  };

  const dropUndefined = (obj) =>
    Object.fromEntries(Object.entries(obj).filter(([, value]) => value !== undefined));

  try {
    const cleanedPrivate = dropUndefined(privateData);
    if (Object.keys(cleanedPrivate).length > 0) {
      await updateDoc(firestoreDoc(db, "users", uid), cleanedPrivate);
    }

    const cleanedPublic = dropUndefined(publicData);
    if (Object.keys(cleanedPublic).length > 0) {
      await setDoc(firestoreDoc(db, "user-public", uid), cleanedPublic, { merge: true });
    }
    return true;
  } catch (err) {
    console.error("[Preferences save error]", err?.code, err?.message, err);
    notifyError(`[${err?.code ?? "unknown"}] ${err?.message}`);
    return false;
  }
}

function normalizePushPreferences(pushPreferences) {
  return {
    pollOpens: pushPreferences?.pollOpens !== false,
    pollCompletes: pushPreferences?.pollCompletes !== false,
    globalChat: pushPreferences?.globalChat === true,
    eventChat: pushPreferences?.eventChat === true,
  };
}

function PushPreferenceChecks({ pushPreferences, heading }) {
  const [formPreferences, setFormPreferences] = useState(
    normalizePushPreferences(pushPreferences)
  );

  useEffect(() => {
    setFormPreferences(normalizePushPreferences(pushPreferences));
  }, [pushPreferences]);

  return (
    <div className="d-flex flex-column gap-1 mt-1">
      <input type="hidden" name="push_prefs_visible" value="1" />
      <p className="mb-1 small fw-semibold">{heading}</p>
      <Form.Check
        id="push_poll_opens"
        type="checkbox"
        name="push_poll_opens"
        checked={formPreferences.pollOpens}
        onChange={(event) => setFormPreferences((prev) => ({ ...prev, pollOpens: event.target.checked }))}
        label="A poll opens"
      />
      <Form.Check
        id="push_poll_completes"
        type="checkbox"
        name="push_poll_completes"
        checked={formPreferences.pollCompletes}
        onChange={(event) => setFormPreferences((prev) => ({ ...prev, pollCompletes: event.target.checked }))}
        label="A poll completes"
      />
      <Form.Check
        id="push_global_chat"
        type="checkbox"
        name="push_global_chat"
        checked={formPreferences.globalChat}
        onChange={(event) => setFormPreferences((prev) => ({ ...prev, globalChat: event.target.checked }))}
        label="A message is sent in global chat"
      />
      <Form.Check
        id="push_event_chat"
        type="checkbox"
        name="push_event_chat"
        checked={formPreferences.eventChat}
        onChange={(event) => setFormPreferences((prev) => ({ ...prev, eventChat: event.target.checked }))}
        label="A message is sent in an event chat I am attending"
      />
    </div>
  );
}

// Admin editing another user: their subscription lives on their own device, so only the
// per-event choices are editable here.
export function AdminPushPreferences({ initialEnabled, pushPreferences }) {
  return (
    <Col xs={12}>
      <Card>
        <Card.Body>
          <div className="d-flex flex-column gap-2">
            <div>
              <h3 className="h5 mb-1">Web Push Notifications</h3>
              <p className="mb-0 text-body-secondary">
                Push is {initialEnabled ? "enabled" : "not enabled"} for this user. Enabling or
                disabling push can only be done by them, on their own device.
              </p>
            </div>
            <PushPreferenceChecks pushPreferences={pushPreferences} heading="Notify this user when:" />
          </div>
        </Card.Body>
      </Card>
    </Col>
  );
}

export function PushPreferences({ uid, initialEnabled, pushPreferences }) {
  const {
    busy,
    disable,
    enable,
    enabled,
    error,
    featureEnabled,
    permission,
    supported,
  } = useWebPushSettings(uid, initialEnabled);

  if (!featureEnabled) {
    return null;
  }

  const permissionLabel =
    permission === "granted"
      ? "Granted"
      : permission === "denied"
        ? "Denied"
        : permission === "default"
          ? "Not requested"
          : "Unsupported";

  return (
    <Col xs={12}>
      <Card>
        <Card.Body>
          <div className="d-flex flex-column gap-2">
            <div>
              <h3 className="h5 mb-1">Web Push Notifications</h3>
              <p className="mb-0 text-body-secondary">
                Receive browser notifications for the events you care about.
              </p>
            </div>
            <div className="small text-body-secondary">
              Status: {enabled ? "Enabled" : "Disabled"} | Permission: {permissionLabel}
            </div>
            {!supported && (
              <div className="text-danger small">
                This browser does not support web push notifications.
              </div>
            )}
            {error && <div className="text-danger small">{error}</div>}
            <div className="d-flex gap-2">
              <Button
                type="button"
                onClick={() => {
                  void enable();
                }}
                disabled={busy || !supported || enabled}
              >
                {busy && !enabled ? "Enabling..." : "Enable Push"}
              </Button>
              <Button
                type="button"
                variant="secondary"
                onClick={() => {
                  void disable();
                }}
                disabled={busy || !supported || !enabled}
              >
                {busy && enabled ? "Disabling..." : "Disable Push"}
              </Button>
            </div>
            {enabled && (
              <PushPreferenceChecks pushPreferences={pushPreferences} heading="Notify me when:" />
            )}
          </div>
        </Card.Body>
      </Card>
    </Col>
  );
}

function PreferencesForm({ method, uid: targetUid, isAdminEditing = false, onCancel }) {
  const authUid = useSelector((state) => state.auth.uid);
  const loggedIn = useSelector((state) => state.auth.loggedIn);
  const uid = targetUid || authUid;
  const [currUserDoc, setCurrUserDoc] = useState({});
  const [publicUserDoc, setPublicUserDoc] = useState({});
  const [isSavingForUser, setIsSavingForUser] = useState(false);

  useEffect(() => {
    if (!loggedIn || !uid) {
      setCurrUserDoc({});
      setPublicUserDoc({});
      return;
    }

    const loadProfiles = async () => {
      const [privateDoc, publicDoc] = await Promise.all([
        getDoc(firestoreDoc(db, "users", uid)),
        getDoc(firestoreDoc(db, "user-public", uid)),
      ]);

      if (!privateDoc.exists()) {
        setCurrUserDoc({});
      } else {
        setCurrUserDoc(privateDoc.data());
      }

      if (publicDoc.exists()) {
        setPublicUserDoc(publicDoc.data());
      } else {
        setPublicUserDoc({});
      }
    };

    loadProfiles();
  }, [loggedIn, uid]);

  const name = loggedIn ? publicUserDoc?.name || currUserDoc?.name || "" : "";
  const notificationEmail = loggedIn ? currUserDoc?.notificationEmail || currUserDoc.email : "";
  const notificationEnabled = loggedIn ? currUserDoc?.notificationEmailEnabled : false;
  const votesVisible = loggedIn ? publicUserDoc?.votesVisible !== false : true;
  const openPollEmail = loggedIn ? currUserDoc?.openPollEmailEnabled : false;
  const webPushEnabled = loggedIn ? currUserDoc?.webPushEnabled === true : false;
  const pushPreferences = loggedIn ? currUserDoc?.pushPreferences ?? null : null;
  const defaultArrivalTime = normalizeArrivalTime(loggedIn ? currUserDoc?.defaultArrivalTime : undefined);
  const authPhotoUrl = useSelector((state) => state.auth.photoUrl);
  const photoUrl = isAdminEditing ? publicUserDoc?.photoUrl || "" : authPhotoUrl;
  const navigate = useNavigate();
  const navigation = useNavigation();

  const isSubmitting = navigation.state === "submitting" || isSavingForUser;

  function cancelHandler() {
    if (onCancel) {
      onCancel();
      return;
    }
    navigate("..");
  }

  async function adminSubmitHandler(event) {
    event.preventDefault();
    const formData = new FormData(event.currentTarget);
    setIsSavingForUser(true);
    const saved = await savePreferences({ uid, formData, currentPhotoUrl: photoUrl });
    setIsSavingForUser(false);
    if (saved && onCancel) {
      onCancel();
    }
  }

  const FormElement = isAdminEditing ? "form" : RouterForm;
  const formProps = isAdminEditing ? { onSubmit: adminSubmitHandler } : { method };

  return (
    <FormElement {...formProps}>
      <Card>
        <Card.Body className="text-body">
          <Row className="g-3">
            <Col xs={12}>
              <Form.Group>
                <Form.Label>My Preferred Name</Form.Label>
                <Form.Control
                  id="name"
                  name="name"
                  type="text"
                  defaultValue={name}
                  title="My preferred name"
                  autoComplete="name"
                />
              </Form.Group>
            </Col>

            <Col xs={12}>
              <Form.Group>
                <Form.Label>Chat Avatar</Form.Label>
                {photoUrl && (
                  <div className="mb-2">
                    <img
                      className="chat-bubble__left"
                      src={photoUrl}
                      alt="user avatar"
                      referrerPolicy="no-referrer"
                    />
                  </div>
                )}
                <Form.Control
                  id="avatar"
                  name="avatar"
                  type="text"
                  defaultValue={photoUrl}
                  title="URL to avatar"
                  autoComplete="photo"
                />
              </Form.Group>
            </Col>

            <Col xs={12}>
              <p className="mb-1">Would you like this app to email you directly?</p>
              <Form.Check
                id="emailme"
                type="checkbox"
                name="emailme"
                defaultChecked={notificationEnabled}
                label="Email Me"
              />
            </Col>

            <Col xs={12}>
              <Form.Group>
                <Form.Label>Email Address</Form.Label>
                <Form.Control
                  id="email"
                  type="text"
                  name="email"
                  title="The email address to use"
                  defaultValue={notificationEmail}
                  autoComplete="email"
                />
              </Form.Group>
            </Col>

            <Col xs={12}>
              <Form.Check
                id="votes_visible"
                type="checkbox"
                name="votes_visible"
                defaultChecked={votesVisible}
                label="Votes Visible to Known Users"
              />
            </Col>

            <Col xs={12}>
              <Form.Check
                id="open_poll_email"
                type="checkbox"
                name="open_poll_email"
                defaultChecked={openPollEmail}
                label="Email me when a poll opens"
              />
            </Col>

            <Col xs={12}>
              <Form.Group>
                <Form.Label>Default arrival time (ETA)</Form.Label>
                <Form.Control
                  id="default_arrival_time"
                  type="time"
                  name="default_arrival_time"
                  defaultValue={defaultArrivalTime}
                  title="Default time to prefill when adding ETA"
                />
              </Form.Group>
            </Col>

            {isAdminEditing ? (
              <AdminPushPreferences initialEnabled={webPushEnabled} pushPreferences={pushPreferences} />
            ) : (
              <PushPreferences uid={uid} initialEnabled={webPushEnabled} pushPreferences={pushPreferences} />
            )}

            <Col xs={12} className="d-flex gap-2">
              <Button type="button" variant="secondary" onClick={cancelHandler} disabled={isSubmitting}>
                Cancel
              </Button>
              <Button type="submit" disabled={isSubmitting}>
                {isSubmitting ? "Submitting..." : "Save"}
              </Button>
            </Col>
          </Row>
        </Card.Body>
      </Card>
    </FormElement>
  );
}

export default PreferencesForm;
