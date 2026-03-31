import React from 'react';
import { Button as BootstrapButton } from 'react-bootstrap';

/**
 * Bootstrap Button Wrapper
 * Uses React-Bootstrap's Button component
 */
const Button = (props) => {
  const variant = props.variant === 'danger' ? 'danger' : 'dark';
  
  return (
    <BootstrapButton
      variant={variant}
      type={props.type || 'button'}
      onClick={props.onClick}
      disabled={props.disabled}
      className={props.className}
    >
      {props.children}
    </BootstrapButton>
  );
};

export default Button;
