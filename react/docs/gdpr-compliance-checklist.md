# GDPR Compliance Checklist for Pub Night Picker

This checklist is tailored to the current React and Firebase implementation. It is intended to turn GDPR work into concrete engineering tasks.

## 1. Immediate Risks To Address

### High priority: user profile overexposure

Current state:

- all authenticated users can read all documents in the `users` collection via Firestore rules
- the client subscribes to the entire `users` collection for logged-in users
- user records include fields such as email address, notification email, notification preferences, and photo URL

Relevant files:

- `firestore.rules`
- `src/hooks/useUsers.js`
- `src/store/usersSlice.js`
- `src/components/pages/ManageUsers.js`
- `src/components/chat/Message.js`

Why this matters:

- GDPR requires data minimization and appropriate access controls
- most authenticated users should not need unrestricted access to other users' email addresses or notification settings

Recommended fix:

- split user data into public and private documents, for example:
  - `users_public/{uid}` for display name and avatar only
  - `users_private/{uid}` for email address, notification preferences, and other private settings
- restrict `users_private/{uid}` so only the user and admins can read it
- update the client so global subscriptions use only the public profile collection
- reserve private profile reads for the current user and admins only

### High priority: no published privacy notice

Current state:

- there is no visible privacy notice route, page, or published document in the app UI

Recommended fix:

- publish a privacy notice based on `docs/privacy-notice-draft.md`
- add an in-app route such as `/privacy`
- link it from navigation, the login area, and preference screens

### High priority: no user self-service data export or deletion

Current state:

- users can change preferences and unsubscribe from emails
- there is no complete export-my-data or delete-my-account flow

Relevant files:

- `src/components/pages/Preferences.js`
- `src/components/pages/NotificationUnsub.js`
- `src/hooks/useSelf.js`
- `src/firebase.js`

Recommended fix:

- add a self-service export flow for the signed-in user
- add a deletion or anonymization workflow that covers all user-linked records

## 2. Legal and Governance Tasks

Complete these before calling the app GDPR-compliant:

- identify the controller legal entity and contact details
- decide and document the lawful basis for each processing purpose
- sign and archive the Google/Firebase data processing terms
- document international transfers and safeguards
- create a Record of Processing Activities
- define an incident response and personal-data breach process
- define a DSAR handling process with response deadlines

## 3. Data Inventory For This Repo

Use this as the starting point for your Record of Processing Activities.

### Authentication and account data

Observed in:

- `src/firebase.js`
- `src/hooks/useSelf.js`
- `src/components/login/Login.js`
- `src/components/login/Register.js`
- `src/components/login/Reset.js`

Data points:

- uid
- name
- email
- auth provider
- photo URL

### User preferences and notification data

Observed in:

- `src/components/pages/Preferences.js`
- `src/components/pages/PreferencesForm.js`
- `src/components/pages/NotificationUnsub.js`

Data points:

- notification email
- notification email enabled flag
- open poll email enabled flag
- votes visible flag
- avatar URL

### Voting and attendance data

Observed in:

- `src/hooks/useVotes.js`
- `src/hooks/useAttendance.js`
- `docs/firestore-data-contract.md`

Data points:

- venue votes linked to user IDs
- attendance linked to user IDs

### Chat data

Observed in:

- `src/components/chat/ChatBox.js`
- `src/components/chat/Message.js`
- `src/components/chat/SendMessage.js`

Data points:

- user-linked messages
- avatar display data

### Roles and administration data

Observed in:

- `firestore.rules`
- `src/hooks/useRoles.js`
- `src/components/pages/ManageUsers.js`

Data points:

- role assignments by user ID
- admin user management access

## 4. Engineering Changes To Implement

### A. Publish privacy information

Suggested work:

- add a new page component at `src/components/pages/Privacy.js`
- add a route in `src/App.js`
- add a navigation link in `src/components/pages/MainNavigation.js`
- add a short privacy section in `src/components/pages/HelpPage.js`

Content should cover:

- what you collect
- why you collect it
- lawful bases
- retention
- rights and contact details

### B. Split public and private user data

Suggested work:

- create a new collection structure for public and private profile data
- move `email`, `notificationEmail`, `notificationEmailEnabled`, `openPollEmailEnabled`, and similar fields out of the globally readable profile collection
- update reads and writes in:
  - `src/firebase.js`
  - `src/hooks/useSelf.js`
  - `src/hooks/useUsers.js`
  - `src/components/pages/Preferences.js`
  - `src/components/pages/PreferencesForm.js`
  - `src/components/pages/ManageUsers.js`
- tighten Firestore rules in `firestore.rules`

### C. Add data subject export

Suggested work:

- add an authenticated export function that gathers the current user's data from:
  - account/profile records
  - votes
  - attendance
  - messages
  - role assignments relevant to the user
- expose it from preferences as a downloadable JSON export

Likely implementation options:

- a callable Cloud Function or trusted server endpoint is preferred
- client-only export is possible but harder to secure and audit

### D. Add account deletion or anonymization

Suggested work:

- add a delete-my-account action from preferences
- delete or anonymize the user's records in:
  - Firebase Auth
  - user profile documents
  - votes arrays
  - attendance arrays
  - chat messages, or anonymize message author data if you need to preserve thread continuity
  - role assignments
- document exactly what is deleted immediately and what is retained temporarily in backups

Important design decision:

- if historical event records must be preserved, replace user IDs with an irreversible pseudonym instead of keeping direct identifiers

### E. Retention automation

Suggested work:

- create scheduled cleanup jobs for:
  - old notification request and acknowledgement records
  - expired chat messages
  - old votes and attendance beyond the retention window
  - stale inactive accounts if your policy allows it

Recommended implementation:

- scheduled Cloud Functions or a trusted scheduled backend job

### F. Consent quality for optional emails

Current state:

- users can opt in or out of email notifications in preferences
- the current UI does not clearly separate core account data from optional marketing-style or service-notification consent language

Relevant files:

- `src/components/pages/PreferencesForm.js`
- `src/components/pages/NotificationUnsub.js`

Suggested work:

- clearly label optional emails as consent-based if that is your chosen lawful basis
- explain what type of email is sent and how often
- make withdrawal of consent immediate and simple
- keep a minimal audit trail showing current consent status and change time if needed

## 5. Firestore Rule Changes To Consider

Current state in `firestore.rules`:

- `users/{userId}` is readable by any authenticated user
- `roles/{role}` is readable by any authenticated user
- votes and attendance are readable by any authenticated user

Questions you should answer:

- do all logged-in users need access to all user emails
- do all logged-in users need access to all role documents
- do all logged-in users need raw vote or attendance data, or only derived event views

Potential rule hardening:

- restrict private user fields to self and admin only
- consider whether some role data should be admin-only
- review whether broad vote and attendance read access is necessary for all authenticated users

## 6. Suggested Delivery Plan

### Phase 1

- publish privacy notice
- add privacy route and links
- document lawful bases and retention schedule

### Phase 2

- split public and private user profile data
- tighten rules
- update client subscriptions

### Phase 3

- implement export-my-data
- implement delete-my-account or anonymization flow

### Phase 4

- add scheduled retention cleanup
- finalize breach response and DSAR playbooks

## 7. Definition Of Done

You should not describe the app as GDPR-compliant until all of the following are true:

- a privacy notice is published and accurate
- lawful bases are documented
- processor and transfer arrangements are documented
- private user data is not exposed to unnecessary users
- users can exercise access, correction, deletion, and consent-withdrawal rights
- retention periods are defined and technically enforced where practical
- breach handling and DSAR handling are documented internally
