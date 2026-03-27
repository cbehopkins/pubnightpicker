import { useCallback, useEffect, useMemo, useState } from "react";
import useAdmin from "../../hooks/useAdmin";
import { useNavigate } from "react-router-dom";
import useUsers from "../../hooks/useUsers";
import { useAllRoles } from "../../hooks/useRoles";
import { db } from "../../firebase";
import { doc, deleteField, updateDoc, setDoc } from "firebase/firestore";
import Modal from "../UI/Modal";


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

async function setCanChat(uid) {
    console.log("Setting canChat status for:", uid);
    const canChatRef = doc(db, "roles", "canChat")
    await setDoc(canChatRef, { [uid]: true }, { merge: true })
}

async function clearCanChat(uid) {
    console.log("Clearing canChat status for:", uid);
    const canChatRef = doc(db, "roles", "canChat")
    await updateDoc(canChatRef, { [uid]: deleteField() })
}

function UIDModal({ uid, onClose }) {
    return (
        <div style={{
            padding: "2rem",
            textAlign: "center",
            minWidth: "350px",
            backgroundColor: "#ffffff",
            borderRadius: "8px",
            boxShadow: "0 4px 12px rgba(0, 0, 0, 0.2)"
        }}>
            <h3 style={{ marginTop: 0, color: "#333", marginBottom: "1.5rem" }}>User ID</h3>
            <p style={{
                marginBottom: "1.5rem",
                wordBreak: "break-all",
                fontFamily: "monospace",
                backgroundColor: "#f9f9f9",
                padding: "1rem",
                borderRadius: "4px",
                fontSize: "0.85rem",
                color: "#000",
                border: "1px solid #ddd",
                lineHeight: "1.6"
            }}>
                {uid}
            </p>
            <div style={{ display: "flex", gap: "0.75rem", justifyContent: "center" }}>
                <button
                    onClick={() => {
                        navigator.clipboard.writeText(uid);
                        onClose();
                    }}
                    style={{
                        padding: "0.6rem 1.2rem",
                        backgroundColor: "#4CAF50",
                        color: "white",
                        border: "none",
                        borderRadius: "4px",
                        cursor: "pointer",
                        fontWeight: "600",
                        fontSize: "0.95rem",
                        transition: "background-color 0.2s"
                    }}
                    onMouseOver={(e) => e.target.style.backgroundColor = "#45a049"}
                    onMouseOut={(e) => e.target.style.backgroundColor = "#4CAF50"}
                >
                    Copy & Close
                </button>
                <button
                    onClick={onClose}
                    style={{
                        padding: "0.6rem 1.2rem",
                        backgroundColor: "#757575",
                        color: "white",
                        border: "none",
                        borderRadius: "4px",
                        cursor: "pointer",
                        fontWeight: "600",
                        fontSize: "0.95rem",
                        transition: "background-color 0.2s"
                    }}
                    onMouseOver={(e) => e.target.style.backgroundColor = "#616161"}
                    onMouseOut={(e) => e.target.style.backgroundColor = "#757575"}
                >
                    Close
                </button>
            </div>
        </div>
    );
}

export default function ManageUsers() {
    const admin = useAdmin();
    const navigate = useNavigate();
    const [selectedUID, setSelectedUID] = useState(null);

    useEffect(() => {
        if (!admin) navigate("/");
    }, [admin, navigate]);

    const users = useUsers();
    const sortedUsers = useMemo(() => {
        return Object.entries(users).sort(([, a], [, b]) => {
            const nameA = (a?.name || a?.email || "").trim();
            const nameB = (b?.name || b?.email || "").trim();
            return nameA.localeCompare(nameB, undefined, { sensitivity: "base" });
        });
    }, [users]);
    const roles = useAllRoles();
    const isAdmin = useCallback((uid) => {
        const adminDict = roles["admin"]
        return roles && adminDict && uid in adminDict && adminDict[uid]
    }, [roles]);
    const isKnown = useCallback((uid) => {
        const knownDict = roles["known"]
        return roles && knownDict && uid in knownDict && knownDict[uid]
    }, [roles]);
    
    const isCanChat = useCallback((uid) => {
        const canChatDict = roles["canChat"]
        return roles && canChatDict && uid in canChatDict && canChatDict[uid]
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

    const handleCanChatClick = useCallback(async (uid, value) => {
        const currentlyCanChat = isCanChat(uid);
        if (currentlyCanChat === value) {
            console.log("Already what it should be, so no change needed")
            return
        }
        if (currentlyCanChat && !value) {
            console.log("Removing canChat field for", uid)
            await clearCanChat(uid);
        }
        if (!currentlyCanChat && value) {
            console.log("Adding canChat field for", uid)
            await setCanChat(uid);
        }
    }, [isCanChat]);

    const handleAutoSyncKnownToChat = useCallback(async () => {
        console.log("Auto-syncing known users to canChat role");
        const knownDict = roles["known"];
        if (!knownDict) {
            console.log("No known users to sync");
            return;
        }
        
        for (const [uid] of Object.entries(knownDict)) {
            if (knownDict[uid] && !isCanChat(uid)) {
                console.log(`Auto-syncing canChat for user: ${uid}`);
                await setCanChat(uid);
            }
        }
        console.log("Auto-sync complete");
    }, [roles, isCanChat]);

    return (<div style={{ padding: "1rem" }}>
        <h1>Manage Users</h1>
        <button 
            onClick={handleAutoSyncKnownToChat}
            style={{
                padding: "0.6rem 1.2rem",
                backgroundColor: "#FF9800",
                color: "white",
                border: "none",
                borderRadius: "4px",
                cursor: "pointer",
                fontWeight: "600",
                marginBottom: "1rem"
            }}
        >
            Auto-sync Known Users to Chat
        </button>
        <table>
            <thead>
                <tr>
                    <th></th>
                    <th>Name</th>
                    <th>Email</th>
                    <th>Admin</th>
                    <th>Known User</th>
                    <th>Can Chat</th>
                </tr>
            </thead>
            <tbody>
                {sortedUsers.map(([key, value]) => {
                    const userIsAdmin = isAdmin(key)
                    const userIsKnown = isKnown(key)
                    const userCanChat = isCanChat(key)
                    return <tr key={key}>
                        <td>
                            <button
                                onClick={() => setSelectedUID(key)}
                                style={{
                                    padding: "0.4rem 0.8rem",
                                    backgroundColor: "#2196F3",
                                    color: "white",
                                    border: "none",
                                    borderRadius: "4px",
                                    cursor: "pointer",
                                    fontSize: "0.9rem"
                                }}
                            >
                                View UID
                            </button>
                        </td>
                        <td>{value.name}</td>
                        <td>{value.email}</td>
                        <td>
                            <input
                                type="checkbox"
                                name="admin"
                                checked={userIsAdmin}
                                onChange={(event) => {
                                    handleAdminClick(key, event.target.checked)
                                }}
                            />
                        </td>
                        <td>
                            <input
                                type="checkbox"
                                name="known"
                                checked={userIsKnown}
                                onChange={(event) => {
                                    handleKnownClick(key, event.target.checked)
                                }}
                            />
                        </td>
                        <td>
                            <input
                                type="checkbox"
                                name="canChat"
                                checked={userCanChat}
                                onChange={(event) => {
                                    handleCanChatClick(key, event.target.checked)
                                }}
                            />
                        </td>
                    </tr>
                })}
            </tbody>
        </table>

        {selectedUID && (
            <Modal>
                <UIDModal uid={selectedUID} onClose={() => setSelectedUID(null)} />
            </Modal>
        )}
    </div>);
};
