import React from 'react';
import { Card as MuiCard, CardContent } from '@mui/material';

/**
 * Material-UI Card Wrapper
 * Uses MUI's Card component
 */
const Card = (props) => {
  return (
    <MuiCard className={props.className} sx={{ boxShadow: 2, borderRadius: 2 }}>
      <CardContent>
        {props.children}
      </CardContent>
    </MuiCard>
  );
};

export default Card;
