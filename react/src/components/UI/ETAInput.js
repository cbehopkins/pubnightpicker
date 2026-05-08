// @ts-check

import { useState } from "react";
import Button from "./Button";
import { DEFAULT_ARRIVAL_TIME } from "../../utils/arrivalTime";

/**
 * @typedef {Object} ETAInputProps
 * @property {string | undefined} userEta - The current ETA value (HH:MM) or undefined if not set
 * @property {(eta: string) => Promise<void>} onSetEta - Called with the new HH:MM string
 * @property {() => Promise<void>} onClearEta - Called when the user removes their ETA
 * @property {string=} className
 * @property {string=} defaultEta
 * @property {"primary" | "secondary" | "success" | "danger" | "warning" | "info" | "light" | "dark" | "outline-primary" | "outline-secondary" | "outline-success" | "outline-danger" | "outline-warning" | "outline-info" | "outline-light" | "outline-dark"=} addButtonVariant
 * @property {string=} addButtonClassName
 */

/**
 * Inline ETA picker shown beneath attendance confirmation.
 * When no ETA is set, shows a time input with a "Set ETA" button.
 * When an ETA is set, shows the time with Edit and Remove buttons.
 *
 * @param {ETAInputProps} props
 */
function ETAInput({ userEta, onSetEta, onClearEta, className, defaultEta = DEFAULT_ARRIVAL_TIME, addButtonVariant = "outline-secondary", addButtonClassName }) {
    const [editing, setEditing] = useState(false);
    const [inputValue, setInputValue] = useState("");

    const openEditor = () => {
        setInputValue(userEta ?? defaultEta);
        setEditing(true);
    };

    const handleSave = async () => {
        if (!inputValue) return;
        await onSetEta(inputValue);
        setEditing(false);
    };

    const handleCancel = () => {
        setEditing(false);
    };

    const handleRemove = async () => {
        await onClearEta();
    };

    if (!userEta && !editing) {
        return (
            <div className={className}>
                <Button
                    type="button"
                    variant={addButtonVariant}
                    className={`btn-sm ${addButtonClassName ?? ""}`.trim()}
                    onClick={openEditor}
                >
                    Add ETA
                </Button>
            </div>
        );
    }

    if (editing) {
        return (
            <div className={`d-flex align-items-center gap-2 flex-wrap ${className ?? ""}`}>
                <label className="form-label mb-0 text-body-secondary small">
                    ETA
                </label>
                <input
                    type="time"
                    className="form-control form-control-sm"
                    style={{ width: "auto" }}
                    value={inputValue}
                    onChange={(e) => setInputValue(e.target.value)}
                    autoFocus
                />
                <Button
                    type="button"
                    variant="outline-primary"
                    className="btn-sm"
                    onClick={handleSave}
                    disabled={!inputValue}
                >
                    Save
                </Button>
                <Button
                    type="button"
                    variant="outline-secondary"
                    className="btn-sm"
                    onClick={handleCancel}
                >
                    Cancel
                </Button>
            </div>
        );
    }

    // ETA is set and we are not editing
    return (
        <div className={`d-flex align-items-center gap-2 flex-wrap ${className ?? ""}`}>
            <span className="text-body-secondary small">
                ETA: <strong>{userEta}</strong>
            </span>
            <Button
                type="button"
                variant="outline-secondary"
                className="btn-sm"
                onClick={openEditor}
            >
                Edit
            </Button>
            <Button
                type="button"
                variant="outline-danger"
                className="btn-sm"
                onClick={handleRemove}
            >
                Remove
            </Button>
        </div>
    );
}

export default ETAInput;
