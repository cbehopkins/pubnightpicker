import React from 'react';
import { Card as RBCard } from 'react-bootstrap';

/**
 * Card Component - Bootstrap Version
 * 
 * Migrated from CSS Module to React-Bootstrap
 * Maintains the same prop interface as the original component
 * 
 * Props:
 * - children: ReactNode - Card content
 * - className: string - Additional CSS classes
 * 
 * Note: Direct child structure is preserved (no Card.Body wrapper added)
 * This allows header/footer/content elements to maintain their existing styles
 */
const Card = (props) => {
    return (
        <RBCard className={props.className}>
            {/* Pass children directly without Card.Body wrapper to maintain structure */}
            {props.children}
        </RBCard>
    );
};

export default Card;
