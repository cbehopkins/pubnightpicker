// @ts-check

import { useState, Children, cloneElement } from "react";
import Modal from "./Modal";
import Button from "./Button";

/**
 * @typedef {Object} ConfirmActionProps
 * @property {string=} confirm_text
 * @property {string=} cancel_text
 * @property {boolean=} confirm_only
 * @property {boolean=} confirm_disabled
 * @property {(params?: unknown) => void | Promise<void>=} on_confirm
 * @property {(params?: unknown) => void | Promise<void>=} on_cancel
 */

/**
 * @typedef {Object} ConfirmModalProps
 * @property {string} title
 * @property {import("react").ReactNode} detail
 * @property {string=} confirm_text
 * @property {string=} cancel_text
 * @property {boolean=} confirm_only
 * @property {boolean=} confirm_disabled
 * @property {(params?: unknown) => void | Promise<void>=} on_confirm
 * @property {(params?: unknown) => void | Promise<void>=} on_cancel
 */

/**
 * @typedef {Object} QuestionRenderProps
 * @property {string=} className
 * @property {import("react").ReactNode=} question
 * @property {import("react").ReactNode=} children
 */

/**
 * @param {QuestionRenderProps} props
 */
export function QuestionRender(props) {
  const [showSelf, setShowSelf] = useState(false)
  return <div className={props.className}>
    <Button onClick={() => { setShowSelf(true) }} type="button">{props.question}</Button>
    {showSelf && <> {
      Children.map(props.children, (child) => {
        if (!child || typeof child === "string" || typeof child === "number" || typeof child === "boolean") {
          return child;
        }
        if (!("props" in child)) {
          return child;
        }
        const childWithProps = /** @type {import("react").ReactElement<ConfirmActionProps>} */ (child);
        return cloneElement(child, {
          on_confirm: async (params) => {
            if (childWithProps.props.on_confirm) {
              await childWithProps.props.on_confirm(params)
            }
            setShowSelf(false)
          },
          on_cancel: async (params) => {
            if (childWithProps.props.on_cancel) {
              await childWithProps.props.on_cancel(params)
            }
            setShowSelf(false)
          },
        })
      })
    }</>}
  </div>
}
/**
 * @param {ConfirmModalProps & QuestionRenderProps} props
 */
export function WithConfirmModal(props) {
  return <QuestionRender className={props.className} question={props.question}>
    <ConfirmModal {...props} />
  </QuestionRender>
}

/**
 * @param {ConfirmModalProps} props
 */
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
