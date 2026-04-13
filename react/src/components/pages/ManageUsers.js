import { useCallback, useEffect, useMemo, useState } from "react";
import { NavLink, useParams } from "react-router-dom";
import { Form, Table } from "react-bootstrap";
import useUsers from "../../hooks/useUsers";
import { useAllRoles } from "../../hooks/useRoles";
import { useIsMobileView } from "../../hooks/useIsMobileView";
import ProtectedRoute from "../ProtectedRoute";
import { db } from "../../firebase";
import { collection, doc, deleteField, onSnapshot, updateDoc, setDoc } from "firebase/firestore";
import Modal from "../UI/Modal";
import Button from "../UI/Button";
import {
    ADMIN_DEFAULT_PERMISSIONS,
    CONSOLIDATED_PERMISSION_COLUMNS,
    KNOWN_DEFAULT_PERMISSIONS,
} from "../../permissions";

const MANAGE_USERS_NARROW_BREAKPOINT = 1200;

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
        <div className="bg-body text-body rounded shadow p-4 border" style={{ minWidth: "350px" }}>
            <h3 className="h5 mb-3">User ID</h3>
            <p className="mb-3 font-monospace border rounded bg-body-tertiary p-3 text-break">{uid}</p>
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

function useManageUsersState() {
    const publicUsers = useUsers();
    const [privateUsers, setPrivateUsers] = useState({});

    useEffect(() => {
        const unsubscribe = onSnapshot(
            collection(db, "users"),
            (snapshot) => {
                const nextPrivateUsers = {};
                snapshot.forEach((userDoc) => {
                    const data = userDoc.data();
                    const normalizedUid = data?.uid || userDoc.id;
                    nextPrivateUsers[normalizedUid] = {
                        ...data,
                        uid: normalizedUid,
                    };
                });
                setPrivateUsers(nextPrivateUsers);
            },
            (error) => {
                console.error("Error loading private users for admin view", error);
                setPrivateUsers({});
            }
        );
        return unsubscribe;
    }, []);

    const mergedUsers = useMemo(() => {
        // users/{uid} is canonical for Manage Users (admin-only page).
        // user-public/{uid} is only used to overlay preferred display fields.
        const merged = {};
        Object.keys(privateUsers).forEach((uid) => {
            merged[uid] = {
                ...(privateUsers[uid] || {}),
                ...(publicUsers[uid] || {}),
                uid,
            };
        });
        return merged;
    }, [privateUsers, publicUsers]);

    const sortedUsers = useMemo(() => {
        return Object.entries(mergedUsers).sort(([, a], [, b]) => {
            const nameA = (a?.name || a?.email || "").trim();
            const nameB = (b?.name || b?.email || "").trim();
            return nameA.localeCompare(nameB, undefined, { sensitivity: "base" });
        });
    }, [mergedUsers]);

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

    return {
        users: mergedUsers,
        sortedUsers,
        isAdmin,
        isKnown,
        hasRole,
        handleAdminClick,
        handleKnownClick,
        handleRoleClick,
        handleAutoSyncKnownToChat,
    };
}

function UserPermissionsChecklist({
    uid,
    isAdmin,
    isKnown,
    hasRole,
    handleAdminClick,
    handleKnownClick,
    handleRoleClick,
}) {
    const userIsAdmin = isAdmin(uid);
    const userIsKnown = isKnown(uid);

    return (
        <div className="d-flex flex-column gap-2">
            <Form.Check
                type="checkbox"
                id={`admin-${uid}`}
                name="admin"
                label="Admin"
                checked={userIsAdmin}
                onChange={(event) => {
                    handleAdminClick(uid, event.target.checked);
                }}
            />
            <Form.Check
                type="checkbox"
                id={`known-${uid}`}
                name="known"
                label="Known User"
                checked={userIsKnown}
                onChange={(event) => {
                    handleKnownClick(uid, event.target.checked);
                }}
            />
            {CONSOLIDATED_PERMISSION_COLUMNS.map((column) => (
                <Form.Check
                    key={column.key}
                    type="checkbox"
                    id={`${column.key}-${uid}`}
                    name={column.key}
                    label={column.label}
                    checked={hasRole(uid, column.key)}
                    onChange={(event) => {
                        handleRoleClick(uid, column.key, event.target.checked);
                    }}
                />
            ))}
        </div>
    );
}

function ManageUsersList({ sortedUsers }) {
    return (
        <div className="list-group">
            {sortedUsers.map(([uid, value]) => {
                const displayName = value?.name || value?.email || uid;
                return (
                    <NavLink
                        key={uid}
                        to={`/manage_users/${uid}`}
                        className="list-group-item list-group-item-action d-flex flex-column"
                    >
                        <span className="fw-semibold">{displayName}</span>
                        <span className="small text-body-secondary text-break">{value?.email || "No email"}</span>
                    </NavLink>
                );
            })}
        </div>
    );
}

function ManageUsersTable({
    sortedUsers,
    isAdmin,
    isKnown,
    hasRole,
    handleAdminClick,
    handleKnownClick,
    handleRoleClick,
    onViewUid,
}) {
    return (
        <div className="table-responsive">
            <Table striped bordered hover size="sm" className="align-middle bg-body">
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
                                        onClick={() => onViewUid(key)}
                                    >
                                        View UID
                                    </Button>
                                </td>
                                <td>
                                    <NavLink to={`/manage_users/${key}`} className="link-primary">
                                        {value?.name || value?.email || key}
                                    </NavLink>
                                </td>
                                <td>{value?.email || "No email"}</td>
                                <td>
                                    <Form.Check
                                        type="checkbox"
                                        name="admin"
                                        checked={isAdmin(key)}
                                        onChange={(event) => {
                                            handleAdminClick(key, event.target.checked);
                                        }}
                                    />
                                </td>
                                <td>
                                    <Form.Check
                                        type="checkbox"
                                        name="known"
                                        checked={isKnown(key)}
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
    );
}

function EmptyUsersNotice() {
    return (
        <div className="alert alert-info" role="alert">
            <h2 className="h6 mb-2">No users to display yet</h2>
            <p className="mb-2">
                Manage Users reads from the <code>user-public</code> collection.
            </p>
            <p className="mb-2">
                Run this one-time backfill command in the browser console while logged in as admin:
            </p>
            <p className="mb-0 small text-break">
                <code>import('/src/dbtools/migrateUserPublicData.js').then((m) =&gt; m.migrateUserPublicData()).then((result) =&gt; console.log('Done:', result));</code>
            </p>
        </div>
    );
}

function ManageUsers() {
    const [selectedUID, setSelectedUID] = useState(null);
    const isMobileView = useIsMobileView(MANAGE_USERS_NARROW_BREAKPOINT);
    const {
        sortedUsers,
        isAdmin,
        isKnown,
        hasRole,
        handleAdminClick,
        handleKnownClick,
        handleRoleClick,
        handleAutoSyncKnownToChat,
    } = useManageUsersState();

    return (
        <div className="container py-3 text-body">
            <h1 className="mb-3">Manage Users</h1>
            <Button
                type="button"
                variant="secondary"
                className="mb-3"
                onClick={handleAutoSyncKnownToChat}
            >
                Auto-sync Known/Admin to Consolidated Permissions
            </Button>

            {sortedUsers.length === 0 ? (
                <EmptyUsersNotice />
            ) : isMobileView ? (
                <ManageUsersList sortedUsers={sortedUsers} />
            ) : (
                <ManageUsersTable
                    sortedUsers={sortedUsers}
                    isAdmin={isAdmin}
                    isKnown={isKnown}
                    hasRole={hasRole}
                    handleAdminClick={handleAdminClick}
                    handleKnownClick={handleKnownClick}
                    handleRoleClick={handleRoleClick}
                    onViewUid={setSelectedUID}
                />
            )}

            {selectedUID && (
                <Modal>
                    <UIDModal uid={selectedUID} onClose={() => setSelectedUID(null)} />
                </Modal>
            )}
        </div>
    );
}

function ManageUserDetailPage() {
    const [selectedUID, setSelectedUID] = useState(null);
    const { userId } = useParams();
    const {
        users,
        handleAdminClick,
        handleKnownClick,
        handleRoleClick,
        isAdmin,
        isKnown,
        hasRole,
    } = useManageUsersState();

    const user = users[userId];
    const hasLoadedUsers = Object.keys(users).length > 0;

    if (!user && !hasLoadedUsers) {
        return (
            <div className="container py-3 text-body">
                <h1 className="mb-3">Manage User</h1>
                <p>Loading user details...</p>
            </div>
        );
    }

    if (!user) {
        return (
            <div className="container py-3 text-body">
                <h1 className="mb-3">Manage User</h1>
                <p className="mb-3">User not found.</p>
                <NavLink to="/manage_users" className="btn btn-secondary">Back to Manage Users</NavLink>
            </div>
        );
    }

    return (
        <div className="container py-3 text-body">
            <div className="d-flex flex-wrap gap-2 align-items-center mb-3">
                <NavLink to="/manage_users" className="btn btn-secondary">Back to Manage Users</NavLink>
                <Button type="button" variant="info" onClick={() => setSelectedUID(userId)}>View UID</Button>
            </div>

            <h1 className="mb-1">Manage User</h1>
            <h2 className="h4 mb-1">{user?.name || user?.email || userId}</h2>
            <p className="text-body-secondary mb-3 text-break">{user?.email || "No email"}</p>

            <UserPermissionsChecklist
                uid={userId}
                isAdmin={isAdmin}
                isKnown={isKnown}
                hasRole={hasRole}
                handleAdminClick={handleAdminClick}
                handleKnownClick={handleKnownClick}
                handleRoleClick={handleRoleClick}
            />

            {selectedUID && (
                <Modal>
                    <UIDModal uid={selectedUID} onClose={() => setSelectedUID(null)} />
                </Modal>
            )}
        </div>
    );
}

export const ManageUserDetail = ProtectedRoute(ManageUserDetailPage, "admin", "/");
export default ProtectedRoute(ManageUsers, "admin", "/");
