import Modal from "./Modal";
import Button from "./Button";
import { useRef } from "react";

function TextModal(props) {
    const inputRef = useRef()
    const confirmText = props.confirm_text || "Do It";
    const cancelText = props.cancel_text || "Run Away";
    const inputType = props.input_type || "text"
    return (
        <Modal>
            <div className="p-3 p-md-4 text-dark bg-white rounded shadow-sm">
                <div className="mb-3">
                    <h5 className="mb-0">{props.title}</h5>
                </div>
                <div className="mb-3">
                    <label className="form-label" htmlFor={props.name}>{props.detail}</label>
                    <input
                        id={props.name}
                        name={props.name}
                        type={inputType}
                        defaultValue={props.default_value}
                        title={props.title}
                        ref={inputRef}
                        className="form-control"
                    />
                </div>
                <div className="d-flex flex-wrap gap-2 justify-content-end">
                    <Button type="button" variant="secondary" onClick={props.on_cancel}>{cancelText}</Button>
                    <Button type="button" onClick={(event) => { props.on_confirm(event, inputRef) }}>{confirmText}</Button>
                </div>
            </div>
        </Modal>
    );
}
export default TextModal;
