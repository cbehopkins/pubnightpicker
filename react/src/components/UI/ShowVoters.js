import Modal from "./Modal";
import Button from "./Button";
import useUsers from "../../hooks/useUsers";
import styles from "./PollVote.module.css";

export default function ShowVoters(params) {
    const users = useUsers();
    const voterList = params.votes || [];

    return <Modal>
        <div className={styles.show_votes}>
            <h3>Current Voters</h3>
            <table >
                <tbody>
                    {voterList.map((voter) => {
                        const voterName = (voter in users && users[voter].name) || "No Name Recorded";
                        return <tr key={voter}>
                            <td>{voterName}</td>
                        </tr>
                    })}
                </tbody>
            </table>
            <Button type="button" variant="secondary" onClick={params.on_cancel}>Close</Button>
        </div>
    </Modal>
}
