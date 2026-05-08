// @ts-check

import styles from "./chat.module.css";

/**
 * A small icon button that shows the notification bell, with a strikethrough
 * when muted. Used in both the event chat page and the event chat modal.
 *
 * @param {{
 *   muted: boolean,
 *   busy?: boolean,
 *   onToggle: () => void,
 *   label?: string,
 * }} props
 */
export default function ChatMuteButton({ muted, busy = false, onToggle, label }) {
    const ariaLabel = label
        ? label
        : muted
            ? "Unmute chat notifications"
            : "Mute chat notifications";

    return (
        <button
            type="button"
            className={styles.eventMuteButton}
            onClick={onToggle}
            disabled={busy}
            aria-label={ariaLabel}
            title={ariaLabel}
        >
            <svg
                viewBox="0 0 24 24"
                className={styles.eventMuteIcon}
                aria-hidden="true"
                focusable="false"
            >
                <path
                    d="M12 4a4 4 0 0 0-4 4v3.4l-1.8 2.4a1 1 0 0 0 .8 1.6h10a1 1 0 0 0 .8-1.6L16 11.4V8a4 4 0 0 0-4-4z"
                    fill="currentColor"
                />
                <path
                    d="M10.2 18a2 2 0 0 0 3.6 0"
                    stroke="currentColor"
                    strokeWidth="1.5"
                    fill="none"
                    strokeLinecap="round"
                />
                {muted && (
                    <path
                        d="M5 5l14 14"
                        stroke="currentColor"
                        strokeWidth="1.8"
                        fill="none"
                        strokeLinecap="round"
                    />
                )}
            </svg>
        </button>
    );
}
