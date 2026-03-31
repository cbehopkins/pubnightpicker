import React from 'react';

/**
 * Materialize CSS Button Wrapper
 * Uses Materialize's button classes
 */
const Button = (props) => {
  let className = 'btn waves-effect waves-light';
  
  // Map danger variant to red background
  if (props.variant === 'danger') {
    className += ' red darken-2';
  } else {
    className += ' purple darken-2';
  }
  
  // Add custom className if provided
  if (props.className) {
    className += ` ${props.className}`;
  }

  return (
    <button
      className={className}
      type={props.type || 'button'}
      onClick={props.onClick}
      disabled={props.disabled}
    >
      {props.children}
    </button>
  );
};

export default Button;
