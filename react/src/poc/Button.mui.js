import React from 'react';
import { Button as MuiButton } from '@mui/material';

/**
 * Material-UI Button Wrapper
 * Uses MUI's Button component with theme integration
 */
const Button = (props) => {
  const variant = props.variant === 'danger' ? 'contained' : 'contained';
  const color = props.variant === 'danger' ? 'error' : 'primary';
  
  return (
    <MuiButton
      variant={variant}
      color={color}
      type={props.type || 'button'}
      onClick={props.onClick}
      disabled={props.disabled}
      className={props.className}
      sx={{ textTransform: 'none' }}
    >
      {props.children}
    </MuiButton>
  );
};

export default Button;
