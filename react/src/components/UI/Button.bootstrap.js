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
const Button = (props) => {
  // Support common Bootstrap variants while keeping primary as the default.
  const allowedVariants = new Set(["primary", "secondary", "success", "danger", "warning", "info", "light", "dark"]);
  const variant = allowedVariants.has(props.variant) ? props.variant : 'primary';

  return (
    <RBButton
      variant={variant}
      type={props.type || 'button'}
      onClick={props.onClick}
      disabled={props.disabled}
      className={props.className}
    >
      {props.children}
    </RBButton>
  );
};

export default Button;
