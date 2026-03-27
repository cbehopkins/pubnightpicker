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
}) {
    const handleCanCome = () => {
        if (canComeSelected) {
            onClear();
            return;
        }
        onSetStatus("canCome");
    };

    const handleCannotCome = () => {
        if (cannotComeSelected) {
            onClear();
            return;
        }
        onSetStatus("cannotCome");
    };

    const canComeClasses = [buttonClassName, canComeSelected && selectedClassName, canComeSelected && canComeSelectedClassName]
        .filter(Boolean)
        .join(" ");
    const cannotComeClasses = [buttonClassName, cannotComeSelected && selectedClassName, cannotComeSelected && cannotComeSelectedClassName]
        .filter(Boolean)
        .join(" ");

    return (
        <div className={className}>
            <button
                className={canComeClasses}
                onClick={handleCanCome}
            >
                {canComeSelected ? canComeSelectedLabel : canComeLabel}
            </button>
            <button
                className={cannotComeClasses}
                onClick={handleCannotCome}
            >
                {cannotComeSelected ? cannotComeSelectedLabel : cannotComeLabel}
            </button>
            {clearMode === "button" && (canComeSelected || cannotComeSelected) && (
                <button className={buttonClassName} onClick={onClear}>
                    {clearLabel}
                </button>
            )}
        </div>
    );
}

export default AttendanceActions;
