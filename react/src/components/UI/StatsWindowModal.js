// @ts-check

import { useEffect, useState } from "react";
import { Form } from "react-bootstrap";
import Modal from "./Modal";
import Button from "./Button";

function parsePositiveInteger(value, fallback) {
  const parsedValue = Number.parseInt(value || "", 10);
  return Number.isFinite(parsedValue) && parsedValue > 0 ? parsedValue : fallback;
}

/**
 * @param {{
 *   title: string,
 *   description: string,
 *   venueLabel?: string,
 *   yearsLabel?: string,
 *   venueInputId?: string,
 *   yearsInputId?: string,
 *   venueLimit: number,
 *   yearCount: number,
 *   onClose: () => void,
 *   onSave: (nextVenueLimit: number, nextYearCount: number) => void,
 * }} props
 */
function StatsWindowModal({
  title,
  description,
  venueLabel = "Venues to show",
  yearsLabel = "Time window (years)",
  venueInputId = "statsVenueLimit",
  yearsInputId = "statsYearCount",
  yearCount,
  venueLimit,
  onClose,
  onSave,
}) {
  const [draftVenueLimit, setDraftVenueLimit] = useState(String(venueLimit));
  const [draftYearCount, setDraftYearCount] = useState(String(yearCount));

  useEffect(() => {
    setDraftVenueLimit(String(venueLimit));
    setDraftYearCount(String(yearCount));
  }, [venueLimit, yearCount]);

  return (
    <Modal onBackdropClick={onClose}>
      <div className="bg-body text-body rounded shadow-sm border p-3 p-md-4">
        <div className="mb-3">
          <h2 className="h5 mb-2">{title}</h2>
          <p className="text-body-secondary mb-0">{description}</p>
        </div>

        <div className="row g-3 mb-3">
          <div className="col-sm-6">
            <Form.Label htmlFor={venueInputId}>{venueLabel}</Form.Label>
            <Form.Control
              id={venueInputId}
              type="number"
              min="1"
              step="1"
              value={draftVenueLimit}
              onChange={(event) => setDraftVenueLimit(event.target.value)}
            />
          </div>
          <div className="col-sm-6">
            <Form.Label htmlFor={yearsInputId}>{yearsLabel}</Form.Label>
            <Form.Control
              id={yearsInputId}
              type="number"
              min="1"
              step="1"
              value={draftYearCount}
              onChange={(event) => setDraftYearCount(event.target.value)}
            />
          </div>
        </div>

        <div className="d-flex flex-wrap justify-content-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>Cancel</Button>
          <Button
            type="button"
            onClick={() => {
              onSave(
                parsePositiveInteger(draftVenueLimit, venueLimit),
                parsePositiveInteger(draftYearCount, yearCount)
              );
            }}
          >
            Apply
          </Button>
        </div>
      </div>
    </Modal>
  );
}

export default StatsWindowModal;
