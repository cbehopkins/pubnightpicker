import { useCallback, useMemo, useState } from "react";
import useUsers from "../../hooks/useUsers";
import { useAllRoles } from "../../hooks/useRoles";
import ProtectedRoute from "../ProtectedRoute";
import { db } from "../../firebase";
import { doc, deleteField, updateDoc, setDoc } from "firebase/firestore";
import Modal from "../UI/Modal";
import {
    ADMIN_DEFAULT_PERMISSIONS,
    CONSOLIDATED_PERMISSION_COLUMNS,
    KNOWN_DEFAULT_PERMISSIONS,
} from "../../permissions";


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

function userHasRole(roles, roleName, uid) {
    if (!roles || !uid) {
        return false;
    }
    const roleDict = roles[roleName];
    return Boolean(roleDict && uid in roleDict && roleDict[uid]);
}

async function setRole(roleName, uid) {
    const roleRef = doc(db, "roles", roleName);
    await setDoc(roleRef, { [uid]: true }, { merge: true });
}

async function clearRole(roleName, uid) {
    const roleRef = doc(db, "roles", roleName);
    await setDoc(roleRef, { [uid]: deleteField() }, { merge: true });
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

function ManageUsers() {
    const [selectedUID, setSelectedUID] = useState(null);

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
        return userHasRole(roles, "admin", uid);
    }, [roles]);
    const isKnown = useCallback((uid) => {
        return userHasRole(roles, "known", uid);
    }, [roles]);

    const hasRole = useCallback((uid, roleName) => {
        return userHasRole(roles, roleName, uid);
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

    const handleRoleClick = useCallback(async (uid, roleName, value) => {
        const currentlyHasRole = hasRole(uid, roleName);
        if (currentlyHasRole === value) {
            return;
        }
        if (value) {
            await setRole(roleName, uid);
            return;
        }
        await clearRole(roleName, uid);
    }, [hasRole]);

    const handleAutoSyncKnownToChat = useCallback(async () => {
        const knownDict = roles["known"] || {};
        const adminDict = roles["admin"] || {};

        const updates = {
            knownUsersScanned: 0,
            adminUsersScanned: 0,
            roleWrites: 0,
        };

        for (const [uid, isKnownUser] of Object.entries(knownDict)) {
            if (!isKnownUser) {
                continue;
            }
            updates.knownUsersScanned += 1;
            for (const roleName of KNOWN_DEFAULT_PERMISSIONS) {
                if (!hasRole(uid, roleName)) {
                    await setRole(roleName, uid);
                    updates.roleWrites += 1;
                }
            }
        }

        for (const [uid, isAdminUser] of Object.entries(adminDict)) {
            if (!isAdminUser) {
                continue;
            }
            updates.adminUsersScanned += 1;
            for (const roleName of ADMIN_DEFAULT_PERMISSIONS) {
                if (!hasRole(uid, roleName)) {
                    await setRole(roleName, uid);
                    updates.roleWrites += 1;
                }
            }
        }

        console.log("Permission backfill complete", updates);
    }, [hasRole, roles]);

    return (<div style={{ padding: "1rem" }}>
        <h1>Manage Users</h1>
        <button
            onClick={handleAutoSyncKnownToChat}
            style={{
                padding: "0.6rem 1.2rem",
                backgroundColor: "#1d3557",
                color: "white",
                border: "none",
                borderRadius: "4px",
                cursor: "pointer",
                fontWeight: "600",
                marginBottom: "1rem"
            }}
        >
            Auto-sync Known/Admin to Consolidated Permissions
        </button>
        <table>
            <thead>
                <tr>
                    <th></th>
                    <th>Name</th>
                    <th>Email</th>
                    <th>Admin</th>
                    <th>Known User</th>
                    {CONSOLIDATED_PERMISSION_COLUMNS.map((column) => {
                        return <th key={column.key}>{column.label}</th>
                    })}
                </tr>
            </thead>
            <tbody>
                {sortedUsers.map(([key, value]) => {
                    const userIsAdmin = isAdmin(key)
                    const userIsKnown = isKnown(key)
                    const userPermissions = CONSOLIDATED_PERMISSION_COLUMNS.reduce((acc, column) => {
                        acc[column.key] = hasRole(key, column.key);
                        return acc;
                    }, {});
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
                        {CONSOLIDATED_PERMISSION_COLUMNS.map((column) => {
                            return (
                                <td key={column.key}>
                                    <input
                                        type="checkbox"
                                        name={column.key}
                                        checked={userPermissions[column.key]}
                                        onChange={(event) => {
                                            handleRoleClick(key, column.key, event.target.checked)
                                        }}
                                    />
                                </td>
                            );
                        })}
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
}

export default ProtectedRoute(ManageUsers, "admin", "/");
