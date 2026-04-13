import { hasCurrentUserPermission, PERMISSIONS } from "../../permissions";

function HelpPage() {
    const canCreatePoll = hasCurrentUserPermission(PERMISSIONS.canCreatePoll);
    const canManagePubs = hasCurrentUserPermission(PERMISSIONS.canManagePubs);

    return (
        <div className="container py-4 py-md-5">
            <section className="mb-4">
                <h1 className="h2 mb-3">Help and FAQ</h1>
                <p className="text-body-secondary mb-0">
                    Guidance for using Pub Night Picker and understanding common account or access questions.
                </p>
            </section>

            <section className="mb-4">
                <h2 className="h5 mb-3">Getting Started</h2>
                <ul className="mb-0">
                    <li>Create an account or sign in to vote and manage preferences.</li>
                    <li>Open Active Polls to vote for a venue.</li>
                    <li>Check Current Events for this week&apos;s selected venue and details.</li>
                </ul>
            </section>

            <section className="mb-4">
                <h2 className="h5 mb-3">Notifications and Preferences</h2>
                <ul className="mb-0">
                    <li>Use My Preferences to control your notification settings.</li>
                    <li>Opt in for updates when polls open and when venue details change.</li>
                    <li>You can change your avatar for the chat window here to a URL you provide.</li>
                    <li>You may select an email address (different from your login email) to receive notifications at. You may disable your email notifications too.</li>
                    <li>You may also change to light/dark mode viewing of this website.</li>
                    <li>For details on personal data handling, see the <a href="/privacy">Privacy Notice</a>.</li>
                </ul>
            </section>

            <section className="mb-4">
                <h2 className="h5 mb-3">Voting and Visibility</h2>
                <ul className="mb-0">
                    <li>Vote in active polls while they are open.</li>
                    <li>Some users may have permission to view vote details after a poll closes.</li>
                    <li>If you are unsure what is visible to others, ask an admin before voting.</li>
                </ul>
            </section>

            <section className="mb-4">
                <h2 className="h5 mb-3">Attendance and Event Planning</h2>
                <ul className="mb-0">
                    <li>After a venue is selected, you can confirm whether you will attend.</li>
                    <li>Your attendance status helps organizers plan for group size and table bookings.</li>
                    <li>If the venue is a restaurant, accurate attendance counts are especially important for reserving adequate tables.</li>
                    <li>Update your attendance status in the Current Events section if your plans change.</li>
                </ul>
            </section>

            <section className="mb-4">
                <h2 className="h5 mb-3">Creating and Completing Polls</h2>
                {canCreatePoll ? (
                    <ul className="mb-0">
                        <li><strong>Creating a poll:</strong> Users with poll creation permissions can open a new poll to start voting on venues.</li>
                        <li><strong>Poll window:</strong> Polls remain open until someone with appropriate permissions closes/completes them.</li>
                        <li><strong>Completing a poll:</strong> To complete a poll, click on the venue name you wish to choose. A dialog box will appear to confirm your selection.</li>
                        <li><strong>When a restaurant is needed:</strong> If the selected pub is marked as not serving food, you will be prompted to choose a restaurant. If one is already added to the poll, it will be automatically selected.</li>
                        <li><strong>Restaurant reservations:</strong> If the venue is a restaurant, please suggest a time to meet there so organizers can make the reservation.</li>
                    </ul>
                ) : (
                    <p className="mb-0 text-body-secondary">
                        You do not have permission to create or complete polls. Contact the site admin if you would like this access.
                    </p>
                )}
            </section>

            <section className="mb-4">
                <h2 className="h5 mb-3">Venues and Permissions</h2>
                {canManagePubs ? (
                    <ul className="mb-0">
                        <li><strong>Venue types:</strong> The system supports three types: Events, Pubs, and Restaurants.</li>
                        <li><strong>Editing venues:</strong> Only users with appropriate permissions can add, edit, or delete venues.</li>
                        <li><strong>Venue roles:</strong> Your account permissions determine which venues you can manage and whether you can propose new ones.</li>
                        <li><strong>Restaurant bookings:</strong> Restaurants require accurate venue details and attendance counts for table reservations.</li>
                    </ul>
                ) : (
                    <p className="mb-0 text-body-secondary">
                        You do not have permission to manage venues. Contact the site admin if you would like this access.
                    </p>
                )}
            </section>

            <section className="mb-4">
                <h2 className="h5 mb-3">Permissions and Access</h2>
                <p className="mb-2 text-body-secondary">
                    Features such as chat, managing venues, creating polls, and managing users are controlled by account permissions.
                </p>
                <p className="mb-0 text-body-secondary">
                    If you desire additional access, contact the site admin.
                </p>
            </section>

            <section>
                <h2 className="h5 mb-3">Still Need Help?</h2>
                <p className="mb-0 text-body-secondary">
                    This website is maintained by Chris H. For bug reports, feature requests, or contributions, <a href="https://github.com/cbehopkins/pubnightpicker" target="_blank" rel="noopener noreferrer">visit us on GitHub</a>. You can also contact Chris directly for support.
                </p>
            </section>
        </div>
    );
}

export default HelpPage;
