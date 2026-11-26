// @ts-check

import { useRef, useState } from "react";
import Button from "./Button";

/** @typedef {"canCome" | "cannotCome"} AttendanceStatus */
/** @typedef {"primary" | "secondary" | "success" | "danger" | "warning" | "info" | "light" | "dark" | "outline-primary" | "outline-secondary" | "outline-success" | "outline-danger" | "outline-warning" | "outline-info" | "outline-light" | "outline-dark"} ButtonVariant */

/**
 * @typedef {Object} AttendanceActionsProps
 * @property {boolean} canComeSelected
 * @property {boolean} cannotComeSelected
 * @property {(status: AttendanceStatus) => Promise<void>} onSetStatus
 * @property {() => Promise<void>} onClear
 * @property {"toggle" | "button"=} clearMode
 * @property {string=} className
 * @property {string=} buttonClassName
 * @property {string=} selectedClassName
 * @property {string=} canComeSelectedClassName
 * @property {string=} cannotComeSelectedClassName
 * @property {string=} canComeLabel
 * @property {string=} canComeSelectedLabel
 * @property {string=} cannotComeLabel
 * @property {string=} cannotComeSelectedLabel
 * @property {string=} clearLabel
 * @property {string=} canComeShortLabel
 * @property {string=} cannotComeShortLabel
 * @property {boolean=} showMobileShortLabels
 * @property {string=} desktopLabelClassName
 * @property {string=} mobileLabelClassName
 * @property {string=} canComeTitle
 * @property {string=} cannotComeTitle
 * @property {() => void=} onAfterAction
 * @property {ButtonVariant=} canComeVariant
 * @property {ButtonVariant=} cannotComeVariant
 * @property {ButtonVariant=} clearVariant
 */

/** @param {AttendanceActionsProps} props */
function AttendanceActions({
    canComeSelected,
    cannotComeSelected,
    onSetStatus,
    onClear,
    clearMode = "toggle",
    className,
    buttonClassName,
    selectedClassName,
    canComeSelectedClassName,
    cannotComeSelectedClassName,
    canComeLabel = "Can come",
    canComeSelectedLabel = "Can come",
    cannotComeLabel = "Cannot come",
    cannotComeSelectedLabel = "Cannot come",
    clearLabel = "Clear response",
    canComeShortLabel = "Yes",
    cannotComeShortLabel = "No",
    showMobileShortLabels = false,
    desktopLabelClassName,
    mobileLabelClassName,
    canComeTitle,
    cannotComeTitle,
    onAfterAction,
    canComeVariant = "success",
    cannotComeVariant = "danger",
    clearVariant = "secondary",
}) {
    const [pendingAction, setPendingAction] = useState(/** @type {"canCome" | "cannotCome" | "clear" | null} */(null));
    const pendingRef = useRef(false);

    /** @param {"canCome" | "cannotCome" | "clear"} action */
    const handleAction = async (action) => {
        if (pendingRef.current) return;
        pendingRef.current = true;
        setPendingAction(action);
        try {
            if (action === "clear" || (action === "canCome" && canComeSelected) || (action === "cannotCome" && cannotComeSelected)) {
                await onClear();
            } else {
                await onSetStatus(action);
            }
            onAfterAction?.();
        } catch {
            return;
        } finally {
            pendingRef.current = false;
            setPendingAction(null);
        }
    };

    const canComeClasses = [buttonClassName, canComeSelected && selectedClassName, canComeSelected && canComeSelectedClassName]
        .filter(Boolean)
        .join(" ");
    const cannotComeClasses = [buttonClassName, cannotComeSelected && selectedClassName, cannotComeSelected && cannotComeSelectedClassName]
        .filter(Boolean)
        .join(" ");

    return (
        <div className={className}>
            <Button
                type="button"
                variant={canComeVariant}
                className={canComeClasses}
                onClick={() => handleAction("canCome")}
                disabled={pendingAction !== null}
                aria-busy={pendingAction === "canCome"}
                title={canComeTitle}
            >
                <span className={desktopLabelClassName}>
                    {pendingAction === "canCome" ? "Saving..." : canComeSelected ? canComeSelectedLabel : canComeLabel}
                </span>
                {showMobileShortLabels && (
                    <span className={mobileLabelClassName}>
                        {pendingAction === "canCome" ? "Saving..." : canComeShortLabel}
                    </span>
                )}
            </Button>
            <Button
                type="button"
                variant={cannotComeVariant}
                className={cannotComeClasses}
                onClick={() => handleAction("cannotCome")}
                disabled={pendingAction !== null}
                aria-busy={pendingAction === "cannotCome"}
                title={cannotComeTitle}
            >
                <span className={desktopLabelClassName}>
                    {pendingAction === "cannotCome" ? "Saving..." : cannotComeSelected ? cannotComeSelectedLabel : cannotComeLabel}
                </span>
                {showMobileShortLabels && (
                    <span className={mobileLabelClassName}>
                        {pendingAction === "cannotCome" ? "Saving..." : cannotComeShortLabel}
                    </span>
                )}
            </Button>
            {clearMode === "button" && (canComeSelected || cannotComeSelected) && (
                <Button
                    type="button"
                    variant={clearVariant}
                    className={buttonClassName}
                    onClick={() => handleAction("clear")}
                    disabled={pendingAction !== null}
                    aria-busy={pendingAction === "clear"}
                >
                    {pendingAction === "clear" ? "Saving..." : clearLabel}
                </Button>
            )}
        </div>
    );
}

export default AttendanceActions;
