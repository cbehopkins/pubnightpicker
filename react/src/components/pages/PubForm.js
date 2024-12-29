import { Form, useNavigate, useNavigation } from "react-router-dom";

import styles from "./PubForm.module.css";

// Positive parameters about a pub - things you want to search for
const PubParams = {
  parking: "Parking",
  food: "Food",
  dog_friend: "Dog Friendly",
  beer_gerden: "Beer Garden",
  out_of_town: "Out Of Town (Cambridge)",
};

// Bad things about a pub, things you want to search for the absence of
const AntiPubParams = { out_of_town: PubParams.out_of_town }

// Generic checkbox component for pub parameters
function PubCheckbox({ name, label, label_mod, pub_object, onChange }) {
  const value =
    pub_object &&
    Object.hasOwn(pub_object, name) &&
    pub_object[name];

  // We can have the same name in multiple filter contexts
  // So multiple labels attached to checkboxes representing different state
  // However a label attaches to any checkbox with the same name
  // So we need a different name on the label/checkbox in those different contexts
  const label_value = `${label_mod || ''}${name}`;
  return (
    <tr>
      <td>
        <input
          id={name}
          type="checkbox"
          name={label_value}
          defaultChecked={value}
          onChange={(event) => {
            onChange && onChange(event, name);
          }}
        />
      </td>
      <td>
        <label htmlFor={label_value}>{label}</label>
      </td>
    </tr>
  );
}

function PubForm({ method, pub_object }) {
  const navigate = useNavigate();
  const navigation = useNavigation();

  const isSubmitting = navigation.state === "submitting";

  function cancelHandler() {
    navigate("..");
  }
  return (
    <Form method={method} className={styles.form}>
      <div>
        <p>
          <label htmlFor="name">Name</label>
          <input
            id="name"
            type="text"
            name="name"
            required
            defaultValue={pub_object ? pub_object.name : ""}
          />
        </p>
        <p>
          <label htmlFor="web_site">Pub Web Site</label>
          <input
            id="web_site"
            type="text"
            name="web_site"
            title="Nice to have the website to show in notification messages"
            defaultValue={pub_object ? pub_object.web_site : ""}
          />
        </p>
        <p>
          <label htmlFor="map">Map</label>
          <input
            id="map"
            type="text"
            name="map"
            title="Usually a google maps link to include in the notification messages"
            defaultValue={pub_object ? pub_object.map : ""}
          />
        </p>
        <p>
          <label htmlFor="address">Address</label>
          <textarea
            id="address"
            type="text"
            name="address"
            title="The address"
            defaultValue={pub_object ? pub_object.address : ""}
          />
        </p>
        <p>
          <label htmlFor="notes">Notes</label>
          <textarea
            id="notes"
            type="text"
            name="notes"
            title="The notes"
            defaultValue={pub_object ? pub_object.notes : ""}
          />
        </p>
        <p>
          <label htmlFor="pubImage">Link to Pub Image</label>
          <input
            id="pubImage"
            type="text"
            name="pubImage"
            title="Pub Image"
            defaultValue={pub_object ? pub_object.pubImage : ""}
            className={styles.pubImage}
          />
        </p>
      </div>
      <table className={styles.checkboxes}>
        <tbody>
          {Object.entries(PubParams).map(([key, value]) => {
            return (
              <PubCheckbox
                key={key}
                name={key}
                label={value}
                pub_object={pub_object}
              />
            );
          })}
        </tbody>
      </table>
      <button type="button" onClick={cancelHandler} disabled={isSubmitting}>
        Cancel
      </button>
      <button type="submit" disabled={isSubmitting} >
        {isSubmitting ? "Submitting..." : "Save"}
      </button>
    </Form>
  );
}

export default PubForm;
export { AntiPubParams, PubParams, PubCheckbox };
