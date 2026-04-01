// @ts-check

import React from 'react';
import { Button as RBButton } from 'react-bootstrap';

/**
 * Button Component - Bootstrap Version
 * 
 * Migrated from CSS Module to React-Bootstrap
 * Maintains the same prop interface as the original component
 * 
 * Props:
 * - children: ReactNode - Button text/content
 * - onClick: function - Click handler
 * - type: string - Button type (button, submit, reset) - defaults to 'button'
 * - disabled: boolean - Disabled state
 * - className: string - Additional CSS classes (for customization)
 * - variant: string (optional) - Bootstrap variant (primary|danger) - defaults to primary
 */
/** @typedef {"primary" | "secondary" | "success" | "danger" | "warning" | "info" | "light" | "dark" | "outline-primary" | "outline-secondary" | "outline-success" | "outline-danger" | "outline-warning" | "outline-info" | "outline-light" | "outline-dark"} ButtonVariant */

/**
 * @typedef {import("react").ButtonHTMLAttributes<HTMLButtonElement> & {
 *   children?: import("react").ReactNode,
 *   variant?: ButtonVariant,
 * }} ButtonProps
 */

const Button = React.forwardRef(
    /**
     * @param {ButtonProps} props
     * @param {import("react").ForwardedRef<HTMLButtonElement>} ref
     */
    (props, ref) => {
        // Support common Bootstrap variants while keeping primary as the default.
        /** @type {Set<ButtonVariant>} */
        const allowedVariants = new Set([
            "primary", "secondary", "success", "danger", "warning", "info", "light", "dark",
            "outline-primary", "outline-secondary", "outline-success", "outline-danger",
            "outline-warning", "outline-info", "outline-light", "outline-dark",
        ]);
        const variant = allowedVariants.has(props.variant) ? props.variant : 'primary';
        const {
            children,
            type,
            onClick,
            disabled,
            className,
            ...restProps
        } = props;

        return (
            <RBButton
                ref={ref}
                variant={variant}
                type={type || 'button'}
                onClick={onClick}
                disabled={disabled}
                className={className}
                {...restProps}
            >
                {children}
            </RBButton>
        );
    });

Button.displayName = 'Button';

export default Button;
