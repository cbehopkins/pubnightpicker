import { useNavigate, defer, useRouteLoaderData } from "react-router-dom";
import { useEffect, useCallback } from "react";
import { useSelector } from "react-redux";
import {
  query,
  getDocs,
  collection,
  where,
  updateDoc,
} from "firebase/firestore";
import { db } from "../../firebase";

export async function loader({ params }) {
  const userId = params.userId;
  return defer({
    user_id: userId,
  });
}

function NotificationUnsub() {
  const uid = useSelector((state) => state.auth.uid);
  const loggedIn = useSelector((state) => state.auth.loggedIn);
  const { user_id } = useRouteLoaderData("user_id");
  const navigate = useNavigate();
  const unsubUser = useCallback(async () => {
    const q = query(collection(db, "users"), where("uid", "==", uid));
    const docs = await getDocs(q);
    const notificationParams = {
      notificationEmailEnabled: false,
    };
    docs.docs.forEach((doc) => {
      try {
        updateDoc(doc.ref, notificationParams);
      } catch (err) {
        console.error(err);
        alert(err.message);
      }
    });
  }, [uid]);

  useEffect(() => {
    if (user_id === uid) {
      console.log("Sucessful uid match, we will unsubscribe you!");
      unsubUser();
      navigate("/");
    }
    if (loggedIn) {
      console.log("Your user id is actually:", uid);
    }
  }, [user_id, uid, loggedIn, unsubUser, navigate]);
  return (
    <>
      <h1>Email Unsubscription</h1>
      <p>
        You attempted to unsubscribe a user that you are not logged in as.
        Please log in and retry
      </p>
    </>
  );
}
export default NotificationUnsub;
