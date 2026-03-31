import React, { useEffect, useState } from "react";
import { useAuthState } from "react-firebase-hooks/auth";
import { useNavigate } from "react-router-dom";
import { Link } from "react-router-dom";
import { Button, Card, Col, Container, Form, Row } from "react-bootstrap";
import { auth } from "../../firebase";
import { sendPasswordResetEmail } from "firebase/auth";
import ConfirmModal from "../UI/ConfirmModal";

function Reset() {
  const [email, setEmail] = useState("");
  const [user, loading] = useAuthState(auth);
  const navigate = useNavigate();
  useEffect(() => {
    if (loading) return;
    if (user) navigate("/");
  }, [user, loading, navigate]);
  const [sentBusy, setSentBusy] = useState(false);
  const [errorString, setErrorString] = useState("");
  const resetString = `Reset email sent to ${email}`;

  const handleSendReset = async () => {
    try {
      await sendPasswordResetEmail(auth, email);
      setSentBusy(true);
    } catch (error) {
      setErrorString(error.message);
    }
  };

  return (
    <Container fluid className="min-vh-100 d-flex align-items-center justify-content-center py-4">
      <Row className="w-100 justify-content-center">
        <Col xs={12} sm={10} md={8} lg={6} xl={5}>
          <Card>
            <Card.Body className="text-body">
              <Card.Title as="h2" className="mb-3">Reset Password</Card.Title>

              {sentBusy && (
                <ConfirmModal
                  title="Reset Send"
                  detail={resetString}
                  confirm_text="Ok"
                  on_confirm={() => setSentBusy(false)}
                  confirm_only={true}
                />
              )}
              {errorString && (
                <ConfirmModal
                  title="Error sending password reset email"
                  detail={errorString}
                  confirm_text="Ok"
                  on_confirm={() => setErrorString("")}
                  confirm_only={true}
                />
              )}

              <Form>
                <Form.Group className="mb-3" controlId="resetEmail">
                  <Form.Label>Email Address</Form.Label>
                  <Form.Control
                    type="email"
                    value={email}
                    onChange={(e) => setEmail(e.target.value)}
                    placeholder="name@example.com"
                    autoComplete="email"
                  />
                </Form.Group>

                <Button type="button" className="w-100 mb-3" onClick={handleSendReset}>
                  Send password reset email
                </Button>
              </Form>

              <div className="small">
                Don&apos;t have an account? <Link to="/register">Register</Link> now.
              </div>
            </Card.Body>
          </Card>
        </Col>
      </Row>
    </Container>
  );
}
export default Reset;
