import { useState, Children, cloneElement } from "react";
import Modal from "./Modal";
import Button from "./Button";

export function QuestionRender(props) {
  const [showSelf, setShowSelf] = useState(false)
  return <div className={props.className}>
    <Button onClick={() => { setShowSelf(true) }} type="button">{props.question}</Button>
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
  const confirmDisabled = Boolean(props?.confirm_disabled);
  return (
    <Modal>
      <div className="p-3 p-md-4 text-body bg-body rounded shadow-sm border">
        <div className="mb-3">
          <h5 className="mb-2">{props.title}</h5>
          <div className="text-secondary">{props.detail}</div>
        </div>
        <div className="d-flex flex-wrap gap-2 justify-content-end">
          {!confirmOnly && (
            <Button type="button" variant="secondary" onClick={props.on_cancel}>{cancelText}</Button>
          )}
          <Button type="button" onClick={props.on_confirm} disabled={confirmDisabled}>{confirmText}</Button>
        </div>
      </div>
    </Modal>
  );
}
export default ConfirmModal;
