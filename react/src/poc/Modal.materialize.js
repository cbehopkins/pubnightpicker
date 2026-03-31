import React from 'react';
import ReactDOM from 'react-dom';

/**
 * Materialize CSS Modal Wrapper
 * Uses Materialize's modal component with custom styling for compatibility
 */
const Modal = (props) => {
  const [isOpen, setIsOpen] = React.useState(true);

  React.useEffect(() => {
    const modalElement = document.getElementById('materialize-modal');
    if (modalElement && window.M) {
      const instance = window.M.Modal.getInstance(modalElement);
      if (instance) {
        if (isOpen) {
          instance.open();
        } else {
          instance.close();
        }
      }
    }
  }, [isOpen]);

  const handleBackdropClick = () => {
    if (props.onBackdropClick) {
      props.onBackdropClick();
    }
  };

  const modalContent = (
    <>
      <div 
        className="modal-overlay" 
        onClick={handleBackdropClick}
        style={{
          position: 'fixed',
          top: 0,
          left: 0,
          width: '100%',
          height: '100%',
          backgroundColor: 'rgba(0, 0, 0, 0.5)',
          zIndex: 998,
          display: isOpen ? 'block' : 'none'
        }}
      />
      <div 
        id="materialize-modal"
        className="modal"
        style={{
          zIndex: 999,
          display: isOpen ? 'block' : 'none',
          top: '50%',
          left: '50%',
          transform: 'translate(-50%, -50%)',
          width: 'min(40rem, calc(100vw - 1rem))',
          maxHeight: '90vh',
          overflow: 'auto'
        }}
      >
        <div className="modal-content">
          {props.children}
        </div>
      </div>
    </>
  );

  const portalElement = document.getElementById('overlay-root');
  return portalElement ? ReactDOM.createPortal(modalContent, portalElement) : modalContent;
};

export default Modal;
