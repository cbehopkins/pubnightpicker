import React, { useEffect, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import {
  auth,
  logInWithEmailAndPassword,
  signInWithGoogle,
} from "../../firebase";
import { useAuthState } from "react-firebase-hooks/auth";
import GoogleSignin from "../../img/btn_google_signin_dark_pressed_web.png";
import styles from "./Login.module.css";

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
  return (
    <div className={styles.login}>
      <div className={styles.loginContainer}>
        <h2>Auth Providers</h2>
        {/* <button className={`${styles.loginBtn} ${styles.loginGoogle}`} onClick={signInWithGoogle}>
          Login with Google
        </button> */}
        <img
          onClick={signInWithGoogle}
          src={GoogleSignin}
          alt="sign in with google"
          type="button"
        />
        {/* <button className={`${styles.loginBtn} ${styles.loginGoogle}`} onClick={signInWithFacebook}>
          Login with Facebook
        </button> */}
      </div>
      <div className={styles.loginContainer}>
        <h2>Login with email</h2>
        <input
          type="text"
          className={styles.loginTextBox}
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          placeholder="E-mail Address"
        />
        <input
          type="password"
          className={styles.loginTextBox}
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          placeholder="Password"
        />
        <button
          className={styles.loginBtn}
          onClick={() => logInWithEmailAndPassword(email, password)}
        >
          Login
        </button>
        <div>
          <Link to="/reset">Forgot Password</Link>
        </div>
        <div>
          Don't have an account? <Link to="/register">Register</Link> now.
        </div>
      </div>
    </div>
  );
}
export default Login;
