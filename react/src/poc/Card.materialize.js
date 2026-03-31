import React from 'react';

/**
 * Materialize CSS Card Wrapper
 * Uses Materialize's card component structure
 */
const Card = (props) => {
  return (
    <div className={`card ${props.className || ''}`}>
      <div className="card-content">
        {props.children}
      </div>
    </div>
  );
};

export default Card;
