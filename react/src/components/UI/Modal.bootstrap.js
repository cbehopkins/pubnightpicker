// @ts-check

import React from 'react';
import ReactDOM from 'react-dom';

/**
 * Modal Component - Bootstrap Version
 * 
 * Migrated from CSS Module to React-Bootstrap
 * 
 * Maintains the same Portal-based rendering pattern to overlay-root.
 * This component is NOT controlled (no show/hide prop) - visibility is managed
 * by conditionally rendering this component itself (or by parent components like
 * ErrorModal, ConfirmModal, TextModal).
 * 
 * Props:
 * - children: ReactNode - Modal content
 * - onBackdropClick: function (optional) - Callback when backdrop is clicked
 * 
 * CSS: Uses Bootstrap modal classes and responsive sizing from bootstrap-overrides.css
 */

/**
 * @typedef {Object} BackdropProps
 * @property {() => void=} onClick
 */

/** @param {BackdropProps} props */
const Backdrop = (props) => {
    return (
        <div
            className="modal-backdrop-portal"
            onClick={props.onClick}
            style={{
                position: 'fixed',
                top: 0,
                left: 0,
                width: '100%',
                height: '100%',
                backgroundColor: 'rgba(0, 0, 0, 0.75)',
                zIndex: 1040, // Bootstrap modal backdrop z-index
                animation: 'fadeIn 0.2s ease-out',
            }}
        />
    );
};

/**
 * @typedef {Object} ModalOverlayProps
 * @property {import('react').ReactNode=} children
 */

/** @param {ModalOverlayProps} props */
const ModalOverlay = (props) => {
    return (
        <div
            className="modal-overlay-portal"
            role="dialog"
            aria-modal="true"
            style={{
                position: 'fixed',
                top: '50%',
                left: '50%',
                transform: 'translate(-50%, -50%)',
                width: 'min(40rem, calc(100vw - 1rem))',
                maxWidth: 'calc(100vw - 1rem)',
                maxHeight: '90vh',
                backgroundColor: 'var(--bs-body-bg)',
                color: 'var(--bs-body-color)',
                borderRadius: '0.5rem',
                border: '1px solid var(--bs-border-color)',
                boxShadow: '0 10px 40px rgba(0, 0, 0, 0.2)',
                zIndex: 1050, // Bootstrap modal z-index
                overflow: 'auto',
                overflowX: 'hidden',
                padding: 'clamp(0.5rem, 2vw, 1.5rem)',
                animation: 'slideDown 0.3s ease-out forwards',
            }}
        >
            {props.children}
        </div>
    );
};

const portalElement = document.getElementById('overlay-root');

/**
 * @typedef {Object} ModalProps
 * @property {import('react').ReactNode=} children
 * @property {() => void=} onBackdropClick
 */

/** @param {ModalProps} props */
const Modal = (props) => {
    const handleBackdropClick = () => {
        if (props.onBackdropClick) {
            props.onBackdropClick();
        }
    };

    if (!portalElement) {
        return null;
    }

    return (
        <>
            {ReactDOM.createPortal(
                <Backdrop onClick={handleBackdropClick} />,
                portalElement
            )}
            {ReactDOM.createPortal(
                <ModalOverlay>{props.children}</ModalOverlay>,
                portalElement
            )}
        </>
    );
};

// Add keyframe animations as a style tag (since inline styles can't have animations)
if (typeof document !== 'undefined' && !document.getElementById('modal-animations')) {
    const style = document.createElement('style');
    style.id = 'modal-animations';
    style.textContent = `
    @keyframes fadeIn {
      from {
        opacity: 0;
      }
      to {
        opacity: 1;
      }
    }
    
    @keyframes slideDown {
      from {
        opacity: 0;
        transform: translate(-50%, -55%);
      }
      to {
        opacity: 1;
        transform: translate(-50%, -50%);
      }
    }
    
    /* Mobile responsive */
    @media (max-width: 480px) {
      .modal-overlay-portal {
        width: calc(100vw - 0.5rem) !important;
        max-width: calc(100vw - 0.5rem) !important;
        padding: 0.25rem !important;
      }
    }
    
    @media (min-width: 768px) {
      .modal-overlay-portal {
        width: min(40rem, calc(100vw - 2rem)) !important;
        max-width: calc(100vw - 2rem) !important;
      }
    }
  `;
    document.head.appendChild(style);
}

export default Modal;
