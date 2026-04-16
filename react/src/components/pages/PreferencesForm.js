import { Form as RouterForm, useNavigate, useNavigation } from "react-router-dom";
import { doc as firestoreDoc, getDoc } from "firebase/firestore";
import { db } from "../../firebase";
import { useSelector } from "react-redux";
import { useEffect, useState } from "react";
import { Card, Col, Form, Row } from "react-bootstrap";
import Button from "../UI/Button";

function PreferencesForm({ method }) {
  const uid = useSelector((state) => state.auth.uid);
  const loggedIn = useSelector((state) => state.auth.loggedIn);
  const [currUserDoc, setCurrUserDoc] = useState({});
  const [publicUserDoc, setPublicUserDoc] = useState({});

  useEffect(() => {
    if (!loggedIn) {
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
  const photoUrl = useSelector((state) => state.auth.photoUrl);
  const navigate = useNavigate();
  const navigation = useNavigation();

  const isSubmitting = navigation.state === "submitting";

  function cancelHandler() {
    navigate("..");
  }

  return (
    <RouterForm method={method}>
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
    </RouterForm>
  );
}

export default PreferencesForm;
