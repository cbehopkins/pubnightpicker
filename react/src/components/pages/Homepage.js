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
        Some functionality needs extra permissions. Ask Chris if you need a
        capability enabled on your account.
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
        <li>Opt in to trusted users being able to see that you voted for that pub when the poll
          closes - note this information is publically available to a well informed user who
          can reverse engineer the database...</li>
      </ul>
      <h1>Permission-Based Features</h1>
      <p>Some features are now granted individually rather than coming from a single broad role.</p>
      <ul>
        <li>Can Chat: join the pub chat and set your avatar via a URL</li>
        <li>Can Add Pub To Poll: add pubs to a live poll and remove pubs from that poll</li>
        <li>Can Show Voters: see who voted for the current event winner or poll options</li>
        <li>Can Create Poll: create polls and delete polls/events</li>
        <li>Can Complete Poll: complete polls and reschedule events</li>
        <li>Can Manage Pubs: create, edit, and delete pubs</li>
        <li>Can Delete Any Message: remove other users' chat messages</li>
      </ul>
      <h1>Admin Features</h1>
      <ul>
        <li>Open the Manage Users page</li>
        <li>Grant and revoke permissions for other users</li>
        <li>Mark users as known users where that label is still useful socially or operationally</li>
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
