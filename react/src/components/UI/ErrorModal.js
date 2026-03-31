import React from 'react';

import Button from './Button';
import Modal from './Modal';

const ModalOverlay = (props) => {
  return (
    <div className="bg-body text-body rounded shadow-sm overflow-hidden border">
      <header className="bg-danger text-white p-3">
        <h2>{props.title}</h2>
      </header>
      <div className="p-3">
        <p>{props.message}</p>
      </div>
      <footer className="p-3 d-flex justify-content-end">
        <Button onClick={props.onConfirm}>Okay</Button>
      </footer>
    </div>
  );
};

const ErrorModal = (props) => {
  return (
    <Modal onBackdropClick={props.onConfirm}>
      <ModalOverlay
        title={props.title}
        message={props.message}
        onConfirm={props.onConfirm}
      />
    </Modal>
  );
};

export default ErrorModal;
