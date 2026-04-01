import { useState } from "react";
import { Button, Card, Col, Form as BsForm, Row, Table } from "react-bootstrap";
import { Form as RouterForm, useNavigate, useNavigation } from "react-router-dom";

const VenueTypes = ["pub", "restaurant", "event"];

// Positive parameters about a pub - things you want to search for
const PubParams = {
  parking: "Parking",
  food: "Food",
  dog_friend: "Dog Friendly",
  beer_gerden: "Beer Garden",
  out_of_town: "Out Of Town (Cambridge)",
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

function PubForm({ method, pub_object }) {
  const navigate = useNavigate();
  const navigation = useNavigation();
  const [venueType, setVenueType] = useState(pub_object ? pub_object.venueType || "pub" : "pub");

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
