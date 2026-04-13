// @ts-check

import Modal from "./Modal";
import Button from "./Button";
import useUsers from "../../hooks/useUsers";
import styles from "./PollVote.module.css";
import { useMemo } from "react";

/** @typedef {{ uid?: string, name?: string }} UserEntry */

/**
 * @param {unknown} value
 * @returns {string}
 */
function normalizeUserId(value) {
    if (value === null || value === undefined) {
        return "";
    }
    return String(value).trim();
}

/**
 * @typedef {Object} ShowAttendanceProps
 * @property {string[]=} voters
 * @property {string[]=} canCome
 * @property {string[]=} cannotCome
 * @property {() => void=} on_cancel
 */

/**
 * @param {ShowAttendanceProps} params
 */
export default function ShowAttendance(params) {
    const users = /** @type {Record<string, UserEntry | undefined>} */ (useUsers());
    const voters = (params.voters || []).map(normalizeUserId).filter(Boolean);
    const canCome = (params.canCome || []).map(normalizeUserId).filter(Boolean);
    const cannotCome = (params.cannotCome || []).map(normalizeUserId).filter(Boolean);

    const votersSet = useMemo(() => new Set(voters), [voters]);
    const canComeSet = useMemo(() => new Set(canCome), [canCome]);
    const cannotComeSet = useMemo(() => new Set(cannotCome), [cannotCome]);

    const usersByUid = useMemo(() => {
        /** @type {Record<string, UserEntry>} */
        const map = {};
        Object.entries(users).forEach(([key, userEntry]) => {
            if (!userEntry) {
                return;
            }
            const keyUid = normalizeUserId(key);
            const entryUid = normalizeUserId(userEntry.uid);
            if (keyUid) {
                map[keyUid] = userEntry;
            }
            if (entryUid) {
                map[entryUid] = userEntry;
            }
        });
        return map;
    }, [users]);

    const allUserIds = [...new Set([...voters, ...canCome, ...cannotCome])];
    const sortedUsers = allUserIds
        .map((id) => ({ id, name: usersByUid[id]?.name || "No Name Recorded" }))
        .sort((a, b) => a.name.localeCompare(b.name));

    const showVoters = voters.length > 0;
    const showCanCome = canCome.length > 0;
    const showCannotCome = cannotCome.length > 0;

    return (
        <Modal>
            <div className={styles.show_votes}>
                <table>
                    <thead>
                        <tr>
                            <th>Name</th>
                            {showVoters && <th>Voted</th>}
                            {showCanCome && <th>Can Come</th>}
                            {showCannotCome && <th>Cannot Come</th>}
                        </tr>
                    </thead>
                    <tbody>
                        {sortedUsers.map(({ id, name }) => (
                            <tr key={id}>
                                <td>{name}</td>
                                {showVoters && (
                                    <td className={votersSet.has(id) ? styles.attendanceCheckYes : ""}>
                                        {votersSet.has(id) ? "✓" : ""}
                                    </td>
                                )}
                                {showCanCome && (
                                    <td className={canComeSet.has(id) ? styles.attendanceCheckYes : ""}>
                                        {canComeSet.has(id) ? "✓" : ""}
                                    </td>
                                )}
                                {showCannotCome && (
                                    <td className={cannotComeSet.has(id) ? styles.attendanceCheckNo : ""}>
                                        {cannotComeSet.has(id) ? "✓" : ""}
                                    </td>
                                )}
                            </tr>
                        ))}
                    </tbody>
                </table>
                <Button type="button" variant="secondary" onClick={params.on_cancel || (() => { })}>Close</Button>
            </div>
        </Modal>
    );
}
