import Modal from "./Modal";
import styles from "./TextModal.module.css";
import { useRef } from "react";

function TextModal(props) {
    const inputRef = useRef()
    const confirmText = props.confirm_text || "Do It";
    const cancelText = props.cancel_text || "Run Away";
    const inputType = props.input_type || "text"
    return (
        <Modal>
            <div className={styles.text_box}>
                <div>
                    <p className={styles.title}>{props.title}</p>
                </div>
                <div>
                    <label htmlFor={props.name}>{props.detail}</label>
                    <input id={props.name} name={props.name} type={inputType} defaultValue={props.default_value} title={props.title} ref={inputRef} />
                </div>
                <div>
                    <button className={styles.ok_button} onClick={(event) => { props.on_confirm(event, inputRef) }}>{confirmText}</button>
                    <button className={styles.cancel} onClick={props.on_cancel}>{cancelText}</button>
                </div>
            </div>
        </Modal>
    );
}
export default TextModal;
