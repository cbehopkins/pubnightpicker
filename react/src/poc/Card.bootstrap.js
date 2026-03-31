import React from 'react';
import { Card as BootstrapCard } from 'react-bootstrap';

/**
 * Bootstrap Card Wrapper
 * Uses React-Bootstrap's Card component
 */
const Card = (props) => {
  return (
    <BootstrapCard className={props.className}>
      <BootstrapCard.Body>
        {props.children}
      </BootstrapCard.Body>
    </BootstrapCard>
  );
};

export default Card;
