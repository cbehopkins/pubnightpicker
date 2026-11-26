# CDD: Account Data Export

**Status:** Behavioural contract agreed; implementation pending  
**Date:** 2026-11-25  
**Related:** [API Architecture](../adr/0012-api-architecture.md);
[Group Membership](../adr/0011-group-membership.md);
[Authenticated Frontend API](../authenticated-api.md);
[Privacy Notice](../../../react/docs/privacy-notice.md)

## 1. Purpose and Scope

Replace the frontend's direct Firestore reads for account data export with a
Last Orders endpoint. The signed-in user downloads a JSON export of their
retained, reliably attributable application data. This is a data-access
migration, not a performance project.

The endpoint assembles the export. The frontend requests it and downloads the
returned JSON. Both existing entry points, the privacy page and the account
deletion flow, use the same endpoint. The existing JSON structure, including
its raw and expanded sections, does not need to be preserved.

## 2. Identity and Data Boundaries

- Require the existing Firebase ID token and derive the user ID from the
  verified principal. Do not accept a client-supplied target user ID as
  authority. Additional reauthentication is not required.
- Return the caller's data only. Do not include another user's UID, profile,
  Votes, Attendance Responses or other personal data. Shared records must be
  filtered or redacted before they are returned.
- Include a record only where there is evidence linking it to the caller. Do
  not infer ownership solely from a matching email address. Unattributable
  records are excluded, and the export must make this limitation clear.
- Exclude secrets, including push subscription authentication secrets. Do not
  expose access tokens, credentials or other sensitive authentication material.
  Identify the omission of secrets in the export.
- Do not include `createdByUid` in the export. Do not copy entire Meet records
  merely to provide participation context.
- Preserve the caller's own historical Votes and Attendance Responses even if
  they are no longer a member of the relevant Group. This does not grant access
  to otherwise restricted Meet or Group details.

## 3. Export Contents

Include the caller's retained, attributable application data, subject to the
boundaries above. This includes:

- Account and profile information, including authentication provider information
  and notification preferences.
- Role assignments, permissions and Group membership information belonging to
  the caller.
- The caller's Votes and Attendance Responses, including their estimated arrival
  times, where recorded.
- The caller's chat messages and relevant participation timestamps.
- User-attributable administrative and application audit records, and
  notification request and acknowledgement records.
- User-associated notification subscription information, excluding secrets.

For Votes and Attendance Responses, include the Meet date and Venue name where
available and authorised. If a Meet or Venue has been deleted, or its context
is inaccessible, retain the caller's participation record and leave the
unavailable context blank. A missing related record is not a failed source read.

Email delivery history and delivery events are excluded in their entirety. This
feature does not extend email-history access or the durable email library.
Infrastructure and provider logs and backups are also out of scope. The export
must not claim to include those excluded sources or records that cannot be
reliably attributed.

Before implementation, inventory the relevant application-owned sources and
their ownership representations. User-owned documents may still contain secrets,
and shared administrative records can contain other people's data. Do not
assume an entire stored document is safe to export because it mentions the
caller. Resolve any cases where free-text fields contain another person's data
without silently weakening either the caller-data or third-party-data boundary.

## 4. API and Processing

HandPump serves an authenticated account export endpoint. Document its route,
response and error contract in OpenAPI. The export's JSON representation may
be chosen for convenient capture of the agreed data; it is not required to
match the existing download. Follow the API architecture's separation of API
representations from persistence structures.

Read authoritative sources as needed for retained historical data; the
on-demand Meet projections are not a complete historical catalogue. The
backend may scan shared collections and filter server-side. Do not move those
reads back into the frontend.

Build the complete response before returning a successful download. If a
required source read fails, return an error rather than a partial export. An
empty, successfully read source is not an error. Reads across sources may occur
at different times; the export is not a cross-system atomic snapshot.

Return `Cache-Control: no-store`. Use the existing API authentication and error
conventions, without exposing internal errors or logging exported personal data,
credentials or bearer tokens.

## 5. Frontend and Documentation

Replace the export utility's Firestore reads and per-Meet vote API calls with a
request to the export endpoint. Download the returned JSON from both existing
entry points. Preserve the existing error handling so a failed export is not
downloaded as though it succeeded.

Update the privacy page's export description to reflect the new contents and
limitations. The download is an application-data export; it is not a claim that
all data held by external providers or in backups is included.

## 6. Verification

- Verify authenticated identity is the sole ownership authority and that a
  request cannot export another user by supplying an ID.
- Exercise all included categories, including role and Group data, notification
  records, admin records, historical participation and attendance ETAs.
- Use shared records containing multiple users and secrets to verify that no
  other user's personal data, `createdByUid` or secrets reach the response.
- Verify unattributable records and email delivery history are excluded, with
  the agreed limitations communicated.
- Verify deleted or inaccessible context does not remove a caller's own
  participation, while an actual required source failure prevents success.
- Verify both frontend entry points download the endpoint's JSON and do not read
  Firestore for the export; verify failed requests do not create a download.
- Cover missing and invalid credentials, no-store responses, empty sources and
  the absence of a cross-source atomicity guarantee.