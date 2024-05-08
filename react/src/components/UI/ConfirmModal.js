import { useState, Children, cloneElement } from "react";
import Modal from "./Modal";
import styles from "./ConfirmModal.module.css";

export function QuestionRender(props) {
  const [showSelf, setShowSelf] = useState(false)
  return <div className={props.className}>
    <button onClick={() => { setShowSelf(true) }}>{props.question}</button>
    {showSelf && <> {
      Children.map(props.children, (child) => {
        return cloneElement(child, {
          on_confirm: async (params) => {
            if (child.props.on_confirm) {
              await child.props.on_confirm(params)
            }
            setShowSelf(false)
          },
          on_cancel: async (params) => {
            if (child.props.on_cancel) {
              await child.props.on_cancel(params)
            }
            setShowSelf(false)
          },
        })
      })
    }</>}
  </div>
}
export function WithConfirmModal(props) {
  return <QuestionRender className={props.className} question={props.question}>
    <ConfirmModal {...props} />
  </QuestionRender>
}

function ConfirmModal(props) {
  const confirmText = props.confirm_text || "Do It";
  const cancelText = props.cancel_text || "Run Away";
  const confirmOnly = props?.confirm_only;
  return (
    <Modal>
      <div className={styles.confirm_dia}>
        <div>
          <p className={styles.title}>{props.title}</p>
          <p className={styles.detail}>{props.detail}</p>
        </div>
        <div>
          <button className={styles.ok_button} onClick={props.on_confirm}>{confirmText}</button>
          {!confirmOnly && <button className={styles.cancel} onClick={props.on_cancel}>{cancelText}</button>}
        </div>
      </div>
    </Modal>
  );
}
export default ConfirmModal;
