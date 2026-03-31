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
  // Map variants: default uses primary (gold), anything with 'danger' uses danger (red)
  const variant = props.variant === 'danger' ? 'danger' : 'primary';

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
