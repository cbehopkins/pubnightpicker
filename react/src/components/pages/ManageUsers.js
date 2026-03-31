import { useCallback, useMemo, useState } from "react";
import { Form, Table } from "react-bootstrap";
import useUsers from "../../hooks/useUsers";
import { useAllRoles } from "../../hooks/useRoles";
import ProtectedRoute from "../ProtectedRoute";
import { db } from "../../firebase";
import { doc, deleteField, updateDoc, setDoc } from "firebase/firestore";
import Modal from "../UI/Modal";
import Button from "../UI/Button";
import {
    ADMIN_DEFAULT_PERMISSIONS,
    CONSOLIDATED_PERMISSION_COLUMNS,
    KNOWN_DEFAULT_PERMISSIONS,
} from "../../permissions";

async function setAdmin(uid) {
    const adminRef = doc(db, "roles", "admin");
    await updateDoc(adminRef, { [uid]: true });
}

async function clearAdmin(uid) {
    const adminRef = doc(db, "roles", "admin");
    await updateDoc(adminRef, { [uid]: deleteField() });
}

async function setKnown(uid) {
    const knownRef = doc(db, "roles", "known");
    await updateDoc(knownRef, { [uid]: true });
}

async function clearKnown(uid) {
    const knownRef = doc(db, "roles", "known");
    await updateDoc(knownRef, { [uid]: deleteField() });
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
        <div className="bg-white text-dark rounded shadow p-4" style={{ minWidth: "350px" }}>
            <h3 className="h5 mb-3">User ID</h3>
            <p className="mb-3 font-monospace border rounded bg-light p-3 text-break">{uid}</p>
            <div className="d-flex gap-2 justify-content-end">
                <Button
                    type="button"
                    variant="success"
                    onClick={() => {
                        navigator.clipboard.writeText(uid);
                        onClose();
                    }}
                >
                    Copy and Close
                </Button>
                <Button type="button" variant="secondary" onClick={onClose}>
                    Close
                </Button>
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

    const isAdmin = useCallback((uid) => userHasRole(roles, "admin", uid), [roles]);
    const isKnown = useCallback((uid) => userHasRole(roles, "known", uid), [roles]);
    const hasRole = useCallback((uid, roleName) => userHasRole(roles, roleName, uid), [roles]);

    const handleAdminClick = useCallback(async (uid, value) => {
        const currentlyAdmin = isAdmin(uid);
        if (currentlyAdmin === value) {
            return;
        }
        if (currentlyAdmin && !value) {
            await clearAdmin(uid);
        }
        if (!currentlyAdmin && value) {
            await setAdmin(uid);
        }
    }, [isAdmin]);

    const handleKnownClick = useCallback(async (uid, value) => {
        const currentlyKnown = isKnown(uid);
        if (currentlyKnown === value) {
            return;
        }
        if (currentlyKnown && !value) {
            await clearKnown(uid);
        }
        if (!currentlyKnown && value) {
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

    return (
        <div className="container py-3 text-dark">
            <h1 className="mb-3">Manage Users</h1>
            <Button
                type="button"
                variant="secondary"
                className="mb-3"
                onClick={handleAutoSyncKnownToChat}
            >
                Auto-sync Known/Admin to Consolidated Permissions
            </Button>

            <div className="table-responsive">
                <Table striped bordered hover size="sm" className="align-middle bg-white">
                    <thead>
                        <tr>
                            <th></th>
                            <th>Name</th>
                            <th>Email</th>
                            <th>Admin</th>
                            <th>Known User</th>
                            {CONSOLIDATED_PERMISSION_COLUMNS.map((column) => (
                                <th key={column.key}>{column.label}</th>
                            ))}
                        </tr>
                    </thead>
                    <tbody>
                        {sortedUsers.map(([key, value]) => {
                            const userIsAdmin = isAdmin(key);
                            const userIsKnown = isKnown(key);
                            const userPermissions = CONSOLIDATED_PERMISSION_COLUMNS.reduce((acc, column) => {
                                acc[column.key] = hasRole(key, column.key);
                                return acc;
                            }, {});

                            return (
                                <tr key={key}>
                                    <td>
                                        <Button
                                            type="button"
                                            variant="info"
                                            size="sm"
                                            onClick={() => setSelectedUID(key)}
                                        >
                                            View UID
                                        </Button>
                                    </td>
                                    <td>{value.name}</td>
                                    <td>{value.email}</td>
                                    <td>
                                        <Form.Check
                                            type="checkbox"
                                            name="admin"
                                            checked={userIsAdmin}
                                            onChange={(event) => {
                                                handleAdminClick(key, event.target.checked);
                                            }}
                                        />
                                    </td>
                                    <td>
                                        <Form.Check
                                            type="checkbox"
                                            name="known"
                                            checked={userIsKnown}
                                            onChange={(event) => {
                                                handleKnownClick(key, event.target.checked);
                                            }}
                                        />
                                    </td>
                                    {CONSOLIDATED_PERMISSION_COLUMNS.map((column) => (
                                        <td key={column.key}>
                                            <Form.Check
                                                type="checkbox"
                                                name={column.key}
                                                checked={userPermissions[column.key]}
                                                onChange={(event) => {
                                                    handleRoleClick(key, column.key, event.target.checked);
                                                }}
                                            />
                                        </td>
                                    ))}
                                </tr>
                            );
                        })}
                    </tbody>
                </Table>
            </div>

            {selectedUID && (
                <Modal>
                    <UIDModal uid={selectedUID} onClose={() => setSelectedUID(null)} />
                </Modal>
            )}
        </div>
    );
}

export default ProtectedRoute(ManageUsers, "admin", "/");
