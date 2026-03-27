import Modal from "./Modal";
import useUsers from "../../hooks/useUsers";
import styles from "./PollVote.module.css";

function AttendanceSection({ title, userIds, users }) {
  if (userIds.length === 0) {
    return null;
  }

  return (
    <div>
      <h3>{title}</h3>
      <table>
        <tbody>
          {userIds.map((userId) => {
            const userName = (userId in users && users[userId].name) || "No Name Recorded";
            return (
              <tr key={`${title}-${userId}`}>
                <td>{userName}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

export default function ShowAttendance(params) {
  const users = useUsers();
  const voters = params.voters || [];
  const canCome = params.canCome || [];
  const cannotCome = params.cannotCome || [];

  return <Modal>
    <div className={styles.show_votes}>
      <AttendanceSection title="Current Voters" userIds={voters} users={users} />
      <AttendanceSection title="Can Come" userIds={canCome} users={users} />
      <AttendanceSection title="Cannot Come" userIds={cannotCome} users={users} />
      <button onClick={params.on_cancel}>Close</button>
    </div>
  </Modal>;
}