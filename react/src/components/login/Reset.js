import React, { useEffect, useState } from "react";
import { useAuthState } from "react-firebase-hooks/auth";
import { useNavigate } from "react-router-dom";
import { Link } from "react-router-dom";
import { auth } from "../../firebase";
import { sendPasswordResetEmail } from "firebase/auth";
import ConfirmModal from "../UI/ConfirmModal";
import "./Reset.module.css";
function Reset() {
  const [email, setEmail] = useState("");
  const [user, loading] = useAuthState(auth);
  const navigate = useNavigate();
  useEffect(() => {
    if (loading) return;
    if (user) navigate("/");
  }, [user, loading, navigate]);
  const [sentBusy, setSentBusy] = useState(false)
  const [errorString, setErrorString] = useState("")
  const resetString = `Reset email sent to ${email}`

  return (
    <div className="reset">
      <div className="reset__container">
        {sentBusy && (
          <ConfirmModal
            title="Reset Send"
            detail={resetString}
            confirm_text="Ok"
            on_confirm={() => setSentBusy(false)}
            confirm_only={true}
          />
        )}
        {errorString && <ConfirmModal
          title="Error sending password reset email"
          detail={errorString}
          confirm_text="Ok"
          on_confirm={() => setErrorString("")}
          confirm_only={true}
        />}
        <input
          type="text"
          className="reset__textBox"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          placeholder="E-mail Address"
        />
        <button
          className="reset__btn"
          onClick={async () => {
            try {
              await sendPasswordResetEmail(auth, email)
              setSentBusy(true)
            } catch (error) {
              setErrorString(error.message)
            }
          }}
        >
          Send password reset email
        </button>
        <div>
          Don't have an account? <Link to="/register">Register</Link> now.
        </div>
      </div>
    </div>
  );
}
export default Reset;
