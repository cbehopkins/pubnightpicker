import { Form, useNavigate, useNavigation } from "react-router-dom";
import { query, getDocs, collection, where } from "firebase/firestore";
import { db } from "../../firebase";
import { useSelector } from "react-redux";
import styles from "./PubForm.module.css";
import { useEffect, useState } from "react";

function PreferencesForm({ method, pub_object, pubs }) {
  const uid = useSelector((state) => state.auth.uid);
  const loggedIn = useSelector((state) => state.auth.loggedIn);
  const [currUserDoc, setCurrUserDoc] = useState({});

  useEffect(() => {
    if (!loggedIn){
        setCurrUserDoc({});
        return
    }
    const q = query(collection(db, "users"), where("uid", "==", uid));
    getDocs(q).then((docs)=>{
        if (docs.length === 0) {
          setCurrUserDoc({});
          return
        }
        setCurrUserDoc(docs.docs[0].data());
    });
  }, [loggedIn, uid, setCurrUserDoc]);
  const name = loggedIn ? currUserDoc?.name : "";
  const notificationEmail = loggedIn ? currUserDoc?.notificationEmail || currUserDoc.email : "";
  const notificationEnabled = loggedIn ? currUserDoc?.notificationEmailEnabled : false;
  const votesVisible = loggedIn ? currUserDoc?.votesVisible : false;
  const openPollEmail = loggedIn ? currUserDoc?.openPollEmailEnabled : false;
  const photoUrl = useSelector((state) => state.auth.photoUrl);
  const navigate = useNavigate();
  const navigation = useNavigation();

  const isSubmitting = navigation.state === "submitting";

  function cancelHandler() {
    navigate("..");
  }
  return (
    <Form method={method} className={styles.form}>
      <p>
        <label htmlFor="name">My Preferred Name</label>
        <input id="name" name="name" type="text" defaultValue={name} title="My preferred name" autoComplete="name" />
      </p>
      <p>
        {photoUrl && <img
          className="chat-bubble__left"
          src={photoUrl}
          alt="user avatar"
          referrerPolicy="no-referrer"
        />}
        <label htmlFor="avatar">Chat Avatar</label>
        <input id="avatar" name="avatar" type="text" defaultValue={photoUrl} title="URL to avatar" autoComplete="photo" />
      </p>
      <p>Would you like this app to email you directly?</p>
      <p className={styles.checkboxes}>
        <input
          id="emailme"
          type="checkbox"
          name="emailme"
          defaultChecked={notificationEnabled}
          onChange={(event) => {}}
        />
        <label htmlFor="emailme">Email Me</label>
      </p>
      <p>
        <label htmlFor="email">Email Address</label>
        <input
          id="email"
          type="text"
          name="email"
          title="The email address to use"
          defaultValue={notificationEmail}
          autoComplete="email"

        />
      </p>
      <p className={styles.checkboxes}>
        <input
          id="votes_visible"
          type="checkbox"
          name="votes_visible"
          defaultChecked={votesVisible}
          onChange={(event) => {}}
        />
        <label htmlFor="votes_visible">Votes Visible to Known Users</label>
      </p>
      <p className={styles.checkboxes}>
        <input
          id="open_poll_email"
          type="checkbox"
          name="open_poll_email"
          defaultChecked={openPollEmail}
          onChange={(event) => {}}
        />
        <label htmlFor="open_poll_email">Email me when a poll opens</label>
      </p>
      <div className={styles.actions}>
        <button type="button" onClick={cancelHandler} disabled={isSubmitting}>
          Cancel
        </button>
        <button disabled={isSubmitting}>
          {isSubmitting ? "Submitting..." : "Save"}
        </button>
      </div>
    </Form>
  );
}

export default PreferencesForm;
