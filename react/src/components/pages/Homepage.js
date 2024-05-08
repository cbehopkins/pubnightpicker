function Homepage() {
  return (
    <div>
      <title>Pubnight Picker Homepage</title>
      <h1>Overview</h1>
      <p>
        This is the ampubnight website. This is used to decide where we will go
        to the pub this week.
      </p>
      <p>
        Most things here are only read only unless you have an account. Please
        register or use your google login...
      </p>
      <p>
        Some functionality needs an admin account - speak to Chris if you want
        this.
      </p>
      <p></p>
      <h1>User Features</h1>
      <ul>
        <li>Opt in to be emailed that the vote has opened</li>
        <li>Vote for the pub to go to</li>
        <li>
          Opt in to be emailed that pub has been chosen/changed at the last
          minute
        </li>
        <li>Opt in to known users being able to see that you voted for that pub when the poll
          closes - note this information is publically available to a well informed user who
          can reverse engineer the database...</li>
        <li>Mastodon bot available <a href="https://botsin.space/@ampubnight">here</a></li>
      </ul>
      <h1>Known User Features</h1>
      <p>To be added as a known user, an admin has to have approved you...</p>
      <ul>
        <li>Add pubs to the poll for consideration</li>
        <li>See who else has voted for the current pub </li>
        <li>Join in the pub chat</li>
        <li>Set your avatar - via a url - to use for chat. </li>
      </ul>
      <h1>Admin Features</h1>
      <ul>
        <li>Edit the pub list</li>
        <li>Edit the pub properties - name, website, address, map etc</li>
        <li>Create/delete a poll</li>
        <li>Complete the Poll - click on the pub's name and you should get a dialog box asking to complete the poll</li>
        <li>Set other users as admin/known users</li>
        <li>Delete other people's messages from chat.</li>
      </ul>
      <h1>Developer Features</h1>
      <ul>
        <li>
          Open-ish database to allow alternate front ends and plugins e.g. the
          notification app
        </li>
      </ul>
      <h1>Notes</h1>
      <p>
        This website is maintained by Chris H - please speak to him if you find
        any issues/have any feature requests.
      </p>
    </div> 
  );
}
export default Homepage;
