import { useCallback, useEffect } from "react";
import useAdmin from "../../hooks/useAdmin";
import { useNavigate } from "react-router-dom";
import useUsers from "../../hooks/useUsers";
import { useAllRoles } from "../../hooks/useRoles";
import { db } from "../../firebase";
import { doc, deleteField, updateDoc } from "firebase/firestore";


async function setAdmin(uid) {
    console.log("Setting admin status for:", uid);
    const adminRef = doc(db, "roles", "admin")
    await updateDoc(adminRef, { [uid]: true })
}

async function clearAdmin(uid) {
    console.log("Setting admin status for:", uid);
    const adminRef = doc(db, "roles", "admin")
    await updateDoc(adminRef, { [uid]: deleteField() })
}
async function setKnown(uid) {
    console.log("Setting known status for:", uid);
    const knownRef = doc(db, "roles", "known")
    await updateDoc(knownRef, { [uid]: true })
}

async function clearKnown(uid) {
    console.log("Clearing known status for:", uid);
    const knownRef = doc(db, "roles", "known")
    await updateDoc(knownRef, { [uid]: deleteField() })
}

export default function ManageUsers() {
    const admin = useAdmin();
    const navigate = useNavigate();
    useEffect(() => {
        if (!admin) navigate("/");
    }, [admin, navigate]);
    const users = useUsers();
    const roles = useAllRoles();
    const isAdmin = useCallback((uid) => {
        const adminDict = roles["admin"]
        return roles && adminDict && uid in adminDict && adminDict[uid]
    }, [roles]);
    const isKnown = useCallback((uid) => {
        const knownDict = roles["known"]
        return roles && knownDict && uid in knownDict && knownDict[uid]
    }, [roles]);

    const handleAdminClick = useCallback(async (uid, value) => {
        const currentlyAdmin = isAdmin(uid);
        if (currentlyAdmin === value) {
            console.log("Already what it should be, so no change needed", value)
            return
        }
        if (currentlyAdmin && !value) {
            console.log("Removing Admin field for", uid)
            await clearAdmin(uid);
        }
        if (!currentlyAdmin && value) {
            console.log("Adding Admin field for ", uid)
            await setAdmin(uid);
        }
    }, [isAdmin]);
    const handleKnownClick = useCallback(async (uid, value) => {
        const currentlyKnown = isKnown(uid);
        if (currentlyKnown === value) {
            console.log("Already what it should be, so no change needed")
            return
        }
        if (currentlyKnown && !value) {
            console.log("Removing Known field for", uid)
            await clearKnown(uid);
        }
        if (!currentlyKnown && value) {
            console.log("Adding Known field for", uid)
            await setKnown(uid);
        }
    }, [isKnown]);
    return (<div>
        <h1> Manage Users</h1>
        <table>
            <thead>
                <tr>
                    <th>UID</th>
                    <th>Name</th>
                    <th>Email</th>
                    <th>Admin</th>
                    <th>Known User</th>
                </tr>
            </thead>
            <tbody>
                {Object.entries(users).map(([key, value]) => {
                    const userIsAdmin = isAdmin(key)
                    const userIsKnown = isKnown(key)
                    return <tr key={key}>
                        <td>{key}</td>
                        <td>{value.name}</td>
                        <td>{value.email}</td>
                        <td>
                            <input
                                id="admin"
                                type="checkbox"
                                name="admin"
                                defaultChecked={userIsAdmin}
                                onChange={(event) => {
                                    handleAdminClick(key, event.target.checked)
                                }}
                            />
                        </td>
                        <td>
                            <input
                                id="known"
                                type="checkbox"
                                name="known"
                                defaultChecked={userIsKnown}
                                onChange={(event) => {
                                    handleKnownClick(key, event.target.checked)
                                }}
                            />
                        </td>
                    </tr>
                })}
            </tbody>
        </table>
    </div>);
};
