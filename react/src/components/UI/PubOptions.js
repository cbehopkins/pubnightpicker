function PubOptions({ pub_parameters, optionText, selectPubHandler }) {

  const sortedPubsByName = Object.entries(pub_parameters)
    .map(([id, pub]) => {
      const sortBy = pub.name.toLowerCase().replace("the ", "");
      return [sortBy, id, pub];
    })
    .sort().map(([, id, pub]) => [id, pub]);

  const optionTextI = optionText || "Select a pub to add here";
  return (
    <select defaultValue="" onChange={selectPubHandler}>
      <option value="">{optionTextI}</option>
      {sortedPubsByName.map(([id, pub]) => (
        <option key={id} value={id}>{pub.name}</option>
      ))}
    </select>
  );
}
export default PubOptions;
