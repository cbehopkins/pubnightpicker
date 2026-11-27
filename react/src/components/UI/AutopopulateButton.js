// @ts-check

import { Spinner } from "react-bootstrap";
import Button from "./Button";

/**
 * @param {{
 *   isLoading: boolean,
 *   isEnabled: boolean,
 *   onAutopopulate: () => void
 * }} props
 */
export default function AutopopulateButton({ isLoading, isEnabled, onAutopopulate }) {
    return (
        <Button
            type="button"
            onClick={onAutopopulate}
            disabled={!isEnabled || isLoading}
            title={
                isLoading
                    ? "Adding eligible venues..."
                    : "Ask Last Orders to add up to 3 eligible venues"
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
