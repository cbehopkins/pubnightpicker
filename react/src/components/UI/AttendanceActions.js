// @ts-check

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
    const handleCanCome = async () => {
        if (canComeSelected) {
            await onClear();
        } else {
            await onSetStatus("canCome");
        }
        onAfterAction?.();
    };

    const handleCannotCome = async () => {
        if (cannotComeSelected) {
            await onClear();
        } else {
            await onSetStatus("cannotCome");
        }
        onAfterAction?.();
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
                onClick={handleCanCome}
                title={canComeTitle}
            >
                <span className={desktopLabelClassName}>
                    {canComeSelected ? canComeSelectedLabel : canComeLabel}
                </span>
                {showMobileShortLabels && (
                    <span className={mobileLabelClassName}>
                        {canComeShortLabel}
                    </span>
                )}
            </Button>
            <Button
                type="button"
                variant={cannotComeVariant}
                className={cannotComeClasses}
                onClick={handleCannotCome}
                title={cannotComeTitle}
            >
                <span className={desktopLabelClassName}>
                    {cannotComeSelected ? cannotComeSelectedLabel : cannotComeLabel}
                </span>
                {showMobileShortLabels && (
                    <span className={mobileLabelClassName}>
                        {cannotComeShortLabel}
                    </span>
                )}
            </Button>
            {clearMode === "button" && (canComeSelected || cannotComeSelected) && (
                <Button
                    type="button"
                    variant={clearVariant}
                    className={buttonClassName}
                    onClick={async () => {
                        await onClear();
                        onAfterAction?.();
                    }}
                >
                    {clearLabel}
                </Button>
            )}
        </div>
    );
}

export default AttendanceActions;
