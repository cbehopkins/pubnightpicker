import React from 'react';
import { Dialog as MuiDialog, DialogContent, Backdrop } from '@mui/material';

/**
 * Material-UI Modal Wrapper
 * Uses MUI's Dialog component (MUI equivalent of Modal)
 */
const Modal = (props) => {
  const [open, setOpen] = React.useState(true);

  const handleClose = () => {
    setOpen(false);
    if (props.onClose) {
      props.onClose();
    }
  };

  return (
    <MuiDialog
      open={open}
      onClose={handleClose}
      maxWidth="sm"
      fullWidth
      component={Backdrop}
      sx={{
        backdropFilter: 'blur(5px)',
        backgroundColor: 'rgba(0, 0, 0, 0.5)',
      }}
    >
      <DialogContent>
        {props.children}
      </DialogContent>
    </MuiDialog>
  );
};

export default Modal;
