import Modal from "./Modal";
import Button from "./Button";
import useUsers from "../../hooks/useUsers";
import styles from "./PollVote.module.css";

export default function ShowAttendance(params) {
    const users = useUsers();
    const voters = params.voters || [];
    const canCome = params.canCome || [];
    const cannotCome = params.cannotCome || [];

    const allUserIds = [...new Set([...voters, ...canCome, ...cannotCome])];
    const sortedUsers = allUserIds
        .map((id) => ({ id, name: (id in users && users[id].name) || "No Name Recorded" }))
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
                                    <td className={voters.includes(id) ? styles.attendanceCheckYes : ""}>
                                        {voters.includes(id) ? "✓" : ""}
                                    </td>
                                )}
                                {showCanCome && (
                                    <td className={canCome.includes(id) ? styles.attendanceCheckYes : ""}>
                                        {canCome.includes(id) ? "✓" : ""}
                                    </td>
                                )}
                                {showCannotCome && (
                                    <td className={cannotCome.includes(id) ? styles.attendanceCheckNo : ""}>
                                        {cannotCome.includes(id) ? "✓" : ""}
                                    </td>
                                )}
                            </tr>
                        ))}
                    </tbody>
                </table>
                <Button type="button" variant="secondary" onClick={params.on_cancel}>Close</Button>
            </div>
        </Modal>
    );
}
