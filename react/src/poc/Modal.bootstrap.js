import React from 'react';
import { Modal as BootstrapModal } from 'react-bootstrap';

/**
 * Bootstrap Modal Wrapper
 * Uses React-Bootstrap's Modal component
 */
const Modal = (props) => {
  const [show, setShow] = React.useState(true);

  React.useEffect(() => {
    if (props.onClose) {
      // Setup close handler if provided
    }
  }, [props]);

  const handleClose = () => {
    setShow(false);
    if (props.onClose) {
      props.onClose();
    }
  };

  return (
    <BootstrapModal 
      show={show} 
      onHide={handleClose}
      centered
      backdrop="static"
    >
      <BootstrapModal.Body>
        {props.children}
      </BootstrapModal.Body>
    </BootstrapModal>
  );
};

export default Modal;
