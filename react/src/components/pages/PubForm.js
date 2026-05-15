import { useState } from "react";
import { Button, Card, Col, Form as BsForm, Row, Table } from "react-bootstrap";
import { Form as RouterForm, useNavigate, useNavigation } from "react-router-dom";

const VenueTypes = ["pub", "restaurant", "event"];
const RecurrenceFrequencies = ["none", "once", "weekly", "monthly", "yearly"];
const WeekdayOptions = [
  ["0", "Sunday"],
  ["1", "Monday"],
  ["2", "Tuesday"],
  ["3", "Wednesday"],
  ["4", "Thursday"],
  ["5", "Friday"],
  ["6", "Saturday"],
];
const MonthlyOrdinalOptions = [
  ["1", "First"],
  ["2", "Second"],
  ["3", "Third"],
  ["4", "Fourth"],
  ["-1", "Last"],
];
const YearMonthOptions = [
  ["1", "January"],
  ["2", "February"],
  ["3", "March"],
  ["4", "April"],
  ["5", "May"],
  ["6", "June"],
  ["7", "July"],
  ["8", "August"],
  ["9", "September"],
  ["10", "October"],
  ["11", "November"],
  ["12", "December"],
];

/**
 * Get ordinal suffix for a day (1st, 2nd, 3rd, 4th, ...)
 * @param {number} day
 * @returns {string}
 */
function getOrdinal(day) {
  if (day > 3 && day < 21) return day + "th";
  switch (day % 10) {
    case 1: return day + "st";
    case 2: return day + "nd";
    case 3: return day + "rd";
    default: return day + "th";
  }
}

/**
 * Format a date string (YYYY-MM-DD) for display with ordinal day (e.g., "28th May 2026")
 * @param {string} dateStr - ISO date string (YYYY-MM-DD)
 * @returns {string} - Formatted date
 */
function formatDateForDisplay(dateStr) {
  if (!dateStr) return "Not yet computed";
  try {
    const date = new Date(dateStr + "T00:00:00Z"); // Parse as UTC
    const locale = typeof navigator !== "undefined" && navigator.language ? navigator.language : undefined;
    // Get day with ordinal
    const day = date.getUTCDate();
    const dayWithOrdinal = getOrdinal(day);
    // Get month and year in locale
    const month = date.toLocaleString(locale, { month: "long", timeZone: "UTC" });
    const year = date.getUTCFullYear();
    return `${dayWithOrdinal} ${month} ${year}`;
  } catch {
    return "Invalid date";
  }
}

// Positive parameters about a pub - things you want to search for
const PubParams = {
  parking: "Parking",
  food: "Food",
  dog_friend: "Dog Friendly",
  beer_gerden: "Beer Garden",
  out_of_town: "Out Of Town (Cambridge)",
  banned: "Banned",
};

// Bad things about a pub, things you want to search for the absence of
const AntiPubParams = { out_of_town: PubParams.out_of_town };

// Generic checkbox component for pub parameters
/**
 * @typedef {Object} PubCheckboxProps
 * @property {string} name
 * @property {string} label
 * @property {string=} label_mod
 * @property {Record<string, unknown>=} pub_object
 * @property {(event: import("react").ChangeEvent<HTMLInputElement>, name: string) => void=} onChange
 */

/** @param {PubCheckboxProps} props */
function PubCheckbox({ name, label, label_mod, pub_object, onChange }) {
  const value = Boolean(pub_object && Object.hasOwn(pub_object, name) && pub_object[name]);

  // We can have the same name in multiple filter contexts
  // So multiple labels attached to checkboxes representing different state
  // However a label attaches to any checkbox with the same name
  // So we need a different name on the label/checkbox in those different contexts
  const label_value = `${label_mod || ""}${name}`;
  return (
    <tr>
      <td className="text-center" style={{ width: "3rem" }}>
        <BsForm.Check
          id={label_value}
          type="checkbox"
          name={label_value}
          defaultChecked={value}
          onChange={(event) => {
            onChange && onChange(event, name);
          }}
          aria-label={label}
        />
      </td>
      <td>
        <BsForm.Label htmlFor={label_value} className="mb-0 text-body-emphasis">
          {label}
        </BsForm.Label>
      </td>
    </tr>
  );
}

function getRecurrenceDefaults(pub_object) {
  const recurrence = pub_object?.recurrence || {};
  const hasWeekday = recurrence.weekday !== undefined && recurrence.weekday !== null;
  const hasMonthDay = recurrence.month_day !== undefined && recurrence.month_day !== null;
  const isMonthlyByWeekday = recurrence.frequency === "monthly" && hasWeekday;
  const isYearlyByWeekday = recurrence.frequency === "yearly" && hasWeekday;
  return {
    frequency: recurrence.frequency || "none",
    start_date: recurrence.start_date || "",
    date: recurrence.date || "",
    interval: recurrence.interval || "1",
    weekday: hasWeekday ? String(recurrence.weekday) : "0",
    nth: recurrence.nth !== undefined && recurrence.nth !== null ? String(recurrence.nth) : "1",
    month:
      recurrence.month !== undefined && recurrence.month !== null ? String(recurrence.month) : "1",
    month_day: hasMonthDay ? String(recurrence.month_day) : "1",
    monthlyMode: isMonthlyByWeekday ? "weekday" : "fixed",
    yearlyMode: isYearlyByWeekday ? "weekday" : "fixed",
  };
}

function RecurrenceFields({ pub_object, venueType, recurrenceFrequency, setRecurrenceFrequency }) {
  const [monthlyMode, setMonthlyMode] = useState(
    getRecurrenceDefaults(pub_object).monthlyMode
  );
  const [yearlyMode, setYearlyMode] = useState(
    getRecurrenceDefaults(pub_object).yearlyMode
  );
  const recurrenceDefaults = getRecurrenceDefaults(pub_object);

  if (venueType !== "event") {
    return null;
  }

  return (
    <Col xs={12}>
      <Card className="border-secondary-subtle">
        <Card.Body>
          <h5 className="mb-2">Event Recurrence</h5>
          <p className="text-body-secondary mb-3">
            Choose a calendar-style recurrence. Only relevant fields are shown.
          </p>

          <Row className="g-3">
            <Col md={4}>
              <BsForm.Group controlId="recurrence_frequency">
                <BsForm.Label>Recurrence Type</BsForm.Label>
                <BsForm.Select
                  name="recurrence_frequency"
                  value={recurrenceFrequency}
                  onChange={(event) => {
                    setRecurrenceFrequency(event.target.value);
                  }}
                >
                  {RecurrenceFrequencies.map((frequency) => {
                    return (
                      <option key={frequency} value={frequency}>
                        {frequency.charAt(0).toUpperCase() + frequency.slice(1)}
                      </option>
                    );
                  })}
                </BsForm.Select>
              </BsForm.Group>
            </Col>

            {recurrenceFrequency === "once" && (
              <Col md={4}>
                <BsForm.Group controlId="recurrence_date">
                  <BsForm.Label>Event Date</BsForm.Label>
                  <BsForm.Control
                    type="date"
                    name="recurrence_date"
                    defaultValue={recurrenceDefaults.date}
                  />
                </BsForm.Group>
              </Col>
            )}

            {recurrenceFrequency === "weekly" && (
              <>
                <Col md={4}>
                  <BsForm.Group controlId="recurrence_weekday">
                    <BsForm.Label>Weekday</BsForm.Label>
                    <BsForm.Select
                      name="recurrence_weekday"
                      defaultValue={recurrenceDefaults.weekday}
                    >
                      {WeekdayOptions.map(([value, label]) => {
                        return (
                          <option key={value} value={value}>
                            {label}
                          </option>
                        );
                      })}
                    </BsForm.Select>
                  </BsForm.Group>
                </Col>
                <Col md={4}>
                  <BsForm.Group controlId="recurrence_interval">
                    <BsForm.Label>Repeat Every (weeks)</BsForm.Label>
                    <BsForm.Control
                      type="number"
                      min="1"
                      step="1"
                      name="recurrence_interval"
                      defaultValue={recurrenceDefaults.interval}
                    />
                  </BsForm.Group>
                </Col>
              </>
            )}

            {recurrenceFrequency === "monthly" && (
              <>
                <Col md={12}>
                  <BsForm.Group>
                    <BsForm.Label>Monthly Pattern</BsForm.Label>
                    <div className="d-flex gap-3">
                      <BsForm.Check
                        type="radio"
                        id="monthly_fixed"
                        name="monthly_mode"
                        label="Fixed day (e.g., 15th of each month)"
                        value="fixed"
                        checked={monthlyMode === "fixed"}
                        onChange={() => setMonthlyMode("fixed")}
                      />
                      <BsForm.Check
                        type="radio"
                        id="monthly_weekday"
                        name="monthly_mode"
                        label="Specific weekday (e.g., last Wednesday)"
                        value="weekday"
                        checked={monthlyMode === "weekday"}
                        onChange={() => setMonthlyMode("weekday")}
                      />
                    </div>
                  </BsForm.Group>
                </Col>

                {monthlyMode === "fixed" && (
                  <Col md={4}>
                    <BsForm.Group controlId="recurrence_month_day">
                      <BsForm.Label>Day of Month</BsForm.Label>
                      <BsForm.Control
                        type="number"
                        min="1"
                        max="31"
                        step="1"
                        name="recurrence_month_day"
                        defaultValue={recurrenceDefaults.month_day}
                      />
                    </BsForm.Group>
                  </Col>
                )}

                {monthlyMode === "weekday" && (
                  <>
                    <Col md={4}>
                      <BsForm.Group controlId="recurrence_weekday">
                        <BsForm.Label>Weekday</BsForm.Label>
                        <BsForm.Select
                          name="recurrence_weekday"
                          defaultValue={recurrenceDefaults.weekday}
                        >
                          {WeekdayOptions.map(([value, label]) => {
                            return (
                              <option key={value} value={value}>
                                {label}
                              </option>
                            );
                          })}
                        </BsForm.Select>
                      </BsForm.Group>
                    </Col>
                    <Col md={4}>
                      <BsForm.Group controlId="recurrence_nth">
                        <BsForm.Label>Which Week</BsForm.Label>
                        <BsForm.Select name="recurrence_nth" defaultValue={recurrenceDefaults.nth}>
                          {MonthlyOrdinalOptions.map(([value, label]) => {
                            return (
                              <option key={value} value={value}>
                                {label}
                              </option>
                            );
                          })}
                        </BsForm.Select>
                      </BsForm.Group>
                    </Col>
                  </>
                )}
              </>
            )}

            {recurrenceFrequency === "yearly" && (
              <>
                <Col md={12}>
                  <BsForm.Group>
                    <BsForm.Label>Yearly Pattern</BsForm.Label>
                    <div className="d-flex gap-3">
                      <BsForm.Check
                        type="radio"
                        id="yearly_fixed"
                        name="yearly_mode"
                        label="Fixed date (e.g., August 23)"
                        value="fixed"
                        checked={yearlyMode === "fixed"}
                        onChange={() => setYearlyMode("fixed")}
                      />
                      <BsForm.Check
                        type="radio"
                        id="yearly_weekday"
                        name="yearly_mode"
                        label="Specific weekday (e.g., last Wednesday in May)"
                        value="weekday"
                        checked={yearlyMode === "weekday"}
                        onChange={() => setYearlyMode("weekday")}
                      />
                    </div>
                  </BsForm.Group>
                </Col>

                <Col md={4}>
                  <BsForm.Group controlId="recurrence_month">
                    <BsForm.Label>Month</BsForm.Label>
                    <BsForm.Select name="recurrence_month" defaultValue={recurrenceDefaults.month}>
                      {YearMonthOptions.map(([value, label]) => {
                        return (
                          <option key={value} value={value}>
                            {label}
                          </option>
                        );
                      })}
                    </BsForm.Select>
                  </BsForm.Group>
                </Col>

                <Col md={4}>
                  <BsForm.Group controlId="recurrence_interval">
                    <BsForm.Label>Repeat Every (years)</BsForm.Label>
                    <BsForm.Control
                      type="number"
                      min="1"
                      step="1"
                      name="recurrence_interval"
                      defaultValue={recurrenceDefaults.interval}
                    />
                  </BsForm.Group>
                </Col>

                {yearlyMode === "fixed" && (
                  <Col md={4}>
                    <BsForm.Group controlId="recurrence_month_day">
                      <BsForm.Label>Day of Month</BsForm.Label>
                      <BsForm.Control
                        type="number"
                        min="1"
                        max="31"
                        step="1"
                        name="recurrence_month_day"
                        defaultValue={recurrenceDefaults.month_day}
                      />
                    </BsForm.Group>
                  </Col>
                )}

                {yearlyMode === "weekday" && (
                  <>
                    <Col md={4}>
                      <BsForm.Group controlId="recurrence_weekday">
                        <BsForm.Label>Weekday</BsForm.Label>
                        <BsForm.Select
                          name="recurrence_weekday"
                          defaultValue={recurrenceDefaults.weekday}
                        >
                          {WeekdayOptions.map(([value, label]) => {
                            return (
                              <option key={value} value={value}>
                                {label}
                              </option>
                            );
                          })}
                        </BsForm.Select>
                      </BsForm.Group>
                    </Col>
                    <Col md={4}>
                      <BsForm.Group controlId="recurrence_nth">
                        <BsForm.Label>Which Week</BsForm.Label>
                        <BsForm.Select name="recurrence_nth" defaultValue={recurrenceDefaults.nth}>
                          {MonthlyOrdinalOptions.map(([value, label]) => {
                            return (
                              <option key={value} value={value}>
                                {label}
                              </option>
                            );
                          })}
                        </BsForm.Select>
                      </BsForm.Group>
                    </Col>
                  </>
                )}
              </>
            )}
          </Row>
          {recurrenceFrequency !== "none" && pub_object?.next_occurrence_date && (
            <Row className="g-3 mt-3 border-top pt-3">
              <Col xs={12}>
                <BsForm.Group>
                  <BsForm.Label className="fw-semibold">Next Event</BsForm.Label>
                  <p className="alert alert-info mb-0 py-2 px-3">
                    {formatDateForDisplay(pub_object.next_occurrence_date)}
                  </p>
                  <small className="text-body-secondary d-block mt-1">
                    This date is computed by the backend. It will be updated when recurrence settings are saved.
                  </small>
                </BsForm.Group>
              </Col>
            </Row>
          )}        </Card.Body>
      </Card>
    </Col>
  );
}

function PubForm({ method, pub_object }) {
  const navigate = useNavigate();
  const navigation = useNavigation();
  const [venueType, setVenueType] = useState(pub_object ? pub_object.venueType || "pub" : "pub");
  const [recurrenceFrequency, setRecurrenceFrequency] = useState(
    pub_object?.recurrence?.frequency || "none"
  );

  const isSubmitting = navigation.state === "submitting";

  function cancelHandler() {
    navigate("..");
  }

  return (
    <RouterForm method={method}>
      <Card>
        <Card.Body className="text-body">
          <Row className="g-3">
            <Col xs={12}>
              <BsForm.Group controlId="venueType">
                <BsForm.Label>Venue Type</BsForm.Label>
                <BsForm.Select
                  name="venueType"
                  value={venueType}
                  onChange={(event) => {
                    setVenueType(event.target.value);
                  }}
                >
                  {VenueTypes.map((type) => {
                    return (
                      <option key={type} value={type}>
                        {type.charAt(0).toUpperCase() + type.slice(1)}
                      </option>
                    );
                  })}
                </BsForm.Select>
              </BsForm.Group>
            </Col>

            <Col xs={12} className="d-flex gap-2">
              <Button type="button" variant="secondary" onClick={cancelHandler} disabled={isSubmitting}>
                Cancel
              </Button>
              <Button type="submit" disabled={isSubmitting}>
                {isSubmitting ? "Submitting..." : "Save"}
              </Button>
            </Col>

            <Col xs={12}>
              <BsForm.Group controlId="name">
                <BsForm.Label>Name</BsForm.Label>
                <BsForm.Control
                  type="text"
                  name="name"
                  required
                  defaultValue={pub_object ? pub_object.name : ""}
                />
              </BsForm.Group>
            </Col>

            <Col xs={12}>
              <BsForm.Group controlId="web_site">
                <BsForm.Label>Venue Web Site</BsForm.Label>
                <BsForm.Control
                  type="text"
                  name="web_site"
                  title="Nice to have the website to show in notification messages"
                  defaultValue={pub_object ? pub_object.web_site : ""}
                />
              </BsForm.Group>
            </Col>

            <Col xs={12}>
              <BsForm.Group controlId="map">
                <BsForm.Label>Map</BsForm.Label>
                <BsForm.Control
                  type="text"
                  name="map"
                  title="Usually a google maps link to include in the notification messages"
                  defaultValue={pub_object ? pub_object.map : ""}
                />
              </BsForm.Group>
            </Col>

            <Col xs={12}>
              <BsForm.Group controlId="address">
                <BsForm.Label>Address</BsForm.Label>
                <BsForm.Control
                  as="textarea"
                  rows={3}
                  name="address"
                  title="The address"
                  defaultValue={pub_object ? pub_object.address : ""}
                />
              </BsForm.Group>
            </Col>

            <Col xs={12}>
              <BsForm.Group controlId="notes">
                <BsForm.Label>Notes</BsForm.Label>
                <BsForm.Control
                  as="textarea"
                  rows={3}
                  name="notes"
                  title="The notes"
                  defaultValue={pub_object ? pub_object.notes : ""}
                />
              </BsForm.Group>
            </Col>

            <Col xs={12}>
              <BsForm.Group controlId="pubImage">
                <BsForm.Label>Link to Venue Image</BsForm.Label>
                <BsForm.Control
                  type="text"
                  name="pubImage"
                  title="Pub Image"
                  defaultValue={pub_object ? pub_object.pubImage : ""}
                />
              </BsForm.Group>
            </Col>

            <RecurrenceFields
              pub_object={pub_object}
              venueType={venueType}
              recurrenceFrequency={recurrenceFrequency}
              setRecurrenceFrequency={setRecurrenceFrequency}
            />

            <Col xs={12}>
              <h5 className="mb-2">Venue Attributes</h5>
              <Table bordered hover size="sm" responsive className="mb-0 align-middle">
                <tbody>
                  {Object.entries(PubParams).map(([key, value]) => {
                    if (venueType === "restaurant" && key === "food") {
                      return null;
                    }

                    return <PubCheckbox key={key} name={key} label={value} pub_object={pub_object} />;
                  })}
                </tbody>
              </Table>
            </Col>
          </Row>
        </Card.Body>
      </Card>
    </RouterForm>
  );
}

export default PubForm;
export { AntiPubParams, PubParams, PubCheckbox };
