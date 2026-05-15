// @ts-check

import { Spinner } from "react-bootstrap";
import Button from "./Button";

/**
 * @param {{
 *   isLoading: boolean,
 *   isEnabled: boolean,
 *   onAutopopulate: () => void,
 *   error?: string | null
 * }} props
 */
export default function AutopopulateButton({ isLoading, isEnabled, onAutopopulate, error }) {
    return (
        <Button
            type="button"
            onClick={onAutopopulate}
            disabled={!isEnabled || isLoading}
            title={
                !isEnabled
                    ? "No viable venues available (all visited in last 4 weeks or already on poll)"
                    : isLoading
                        ? "Loading venue suggestions..."
                        : "Auto-populate 3 unique venues when possible; otherwise add all remaining viable venues"
            }
        >
            {isLoading ? (
                <>
                    <Spinner animation="border" size="sm" className="me-2" />
                    Loading...
                </>
            ) : (
                "Autopopulate"
            )}
        </Button>
    );
}
