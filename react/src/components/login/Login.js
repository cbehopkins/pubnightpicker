import React, { useEffect, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { Button, Card, Col, Container, Form, Image, Row } from "react-bootstrap";
import {
  auth,
  logInWithEmailAndPassword,
  signInWithGoogle,
} from "../../firebase";
import { useAuthState } from "react-firebase-hooks/auth";
import GoogleSignin from "../../img/btn_google_signin_dark_pressed_web.png";

function Login() {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [user, loading] = useAuthState(auth);
  const navigate = useNavigate();
  useEffect(() => {
    if (loading) {
      // maybe trigger a loading screen
      return;
    }
    if (user) navigate("/");
  }, [user, loading, navigate]);

  const handleEmailLogin = () => {
    logInWithEmailAndPassword(email, password);
  };

  return (
    <Container fluid className="min-vh-100 d-flex align-items-center justify-content-center py-4">
      <Row className="w-100 justify-content-center g-3" style={{ maxWidth: "900px" }}>
        <Col xs={12} md={6}>
          <Card className="h-100">
            <Card.Body className="d-flex flex-column align-items-center text-body gap-3">
              <Card.Title as="h2" className="mb-2">Auth Providers</Card.Title>
              <Button
                type="button"
                variant="outline-secondary"
                className="p-0 border-0 bg-transparent"
                onClick={signInWithGoogle}
                aria-label="Sign in with Google"
              >
                <Image
                  src={GoogleSignin}
                  alt="Sign in with Google"
                  fluid
                  rounded
                />
              </Button>
            </Card.Body>
          </Card>
        </Col>

        <Col xs={12} md={6}>
          <Card className="h-100">
            <Card.Body className="text-body">
              <Card.Title as="h2" className="mb-3">Login with Email</Card.Title>
              <Form>
                <Form.Group className="mb-3" controlId="loginEmail">
                  <Form.Label>Email Address</Form.Label>
                  <Form.Control
                    type="email"
                    value={email}
                    onChange={(e) => setEmail(e.target.value)}
                    placeholder="name@example.com"
                    autoComplete="email"
                  />
                </Form.Group>

                <Form.Group className="mb-3" controlId="loginPassword">
                  <Form.Label>Password</Form.Label>
                  <Form.Control
                    type="password"
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    placeholder="Password"
                    autoComplete="current-password"
                  />
                </Form.Group>

                <Button type="button" className="w-100 mb-3" onClick={handleEmailLogin}>
                  Login
                </Button>
              </Form>

              <div className="small mb-2">
                <Link to="/reset">Forgot Password</Link>
              </div>
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
export default Login;
