# Authenticated Frontend API

## Scope

React calls Last Orders directly over HTTPS using Firebase Authentication ID
tokens. Firestore is not the request or response transport. There is no Python
implementation. Existing database listeners continue independently.

`POST /api/ping` is a synchronous transport and identity diagnostic, not a
durable application event. It creates no Cellar work or database records. Future
business operations must retain their normal durable acceptance semantics.

## Trust Boundary

The frontend obtains an ID token from the existing Firebase user with
`getIdToken()` and sends it in `Authorization: Bearer <ID token>`. The Go Firebase
Admin SDK verifies its signature, expiry, issuer, audience and subject against
the configured project. API handlers receive a verified principal through
`firebaseauth.FromContext`; JSON fields, Redux state and frontend permissions are
not authentication evidence. Token verification has a five-second deadline.

Any Firebase-authenticated user may ping. The diagnostics page remains
admin-only, but that is a UI restriction, not backend authorisation. Future
endpoints must authorise their operations explicitly. For email status, filter
ownership using the verified UID, never a caller-supplied UID or email address.
Durable operations must copy the trusted identity into their typed payload;
request contexts do not survive a Cellar hand-off.

Standard `VerifyIDToken` is used, without immediate revocation or disabled-user
checks. Previously issued valid tokens can remain accepted until expiry.
Immediate checks would require a Firebase Auth lookup. Public signing certificates
may be fetched and cached; no Firestore lookup is needed for authentication.

**The existing `POST /log` endpoint is unchanged and unauthenticated.** This
feature does not secure every existing backend route. Do not expose `/log` through
a public proxy unless its existing behaviour is intended.

## Ping Contract

Request:

```http
POST /api/ping
Authorization: Bearer <Firebase ID token>
Content-Type: application/json

{"request_id":"bca1207e-0519-4512-b9b5-c1b8a1d6fd00"}
```

`request_id` is a canonical lowercase UUID used only for correlation. It is not
an identity or idempotency key. Bodies are limited to 1 KiB. Unknown fields,
invalid JSON, trailing JSON and invalid IDs are rejected.

Success is HTTP `200`, with `Cache-Control: no-store`:

```json
{"status":"ok","uid":"<verified Firebase UID>","request_id":"bca1207e-0519-4512-b9b5-c1b8a1d6fd00"}
```

Before responding, the backend emits one structured `authenticated API ping`
log containing `uid` and `request_id`. Bearer tokens are never logged.

Failures use `{"error":{"code":"...","message":"..."}}`:

| HTTP | Code | Meaning |
| --- | --- | --- |
| 401 | `unauthenticated` | Missing, malformed, expired, invalid or wrong-project token |
| 503 | `auth_unavailable` | Verification infrastructure is unavailable |
| 400 | `invalid_request` | Invalid JSON, request shape or correlation ID |
| 413 | `request_too_large` | Body exceeds 1 KiB |
| 415 | `unsupported_media_type` | Content type is not `application/json` |
| 403 | `forbidden_origin` / `forbidden_preflight` | Browser origin, method or headers are not allowed |

Authentication occurs before payload processing. Invalid requests do not produce
a successful ping log. Infrastructure details are not returned to clients.

## Email History

`POST /api/email-history` uses the same authentication, strict `request_id` JSON
payload, size limit, correlation and no-store headers as ping. No UID, recipient,
state filter or pagination arguments are accepted from the caller.

The endpoint returns the latest 20 recipient sends owned by the verified UID,
ordered by the time the durable send was created, newest first. The success
envelope contains `status`, `uid`, `request_id` and an `entries` array. Each entry
contains `subject`, `recipient`, `state`, `created_at` and nullable `submitted_at`.
Timestamps use UTC RFC 3339. Provider IDs and internal idempotency tokens are not
exposed. An empty history returns `entries: []`; a database failure returns HTTP
503 with code `history_unavailable` without internal details. Queries have a
five-second deadline and do not create durable work or send email.

Ownership is recorded from trusted UIDs when new test, poll-open and personal
poll-completion sends are created. Mailing-list sends and historical records
without ownership are excluded. Changing an address does not change ownership
of previous sends; setting somebody else's address cannot reveal their history.

## Venue and Any-Venue Attendance Responses

Authenticated attendance snapshots and live updates use Last Orders:

```http
GET    /api/meet/{meet_id}/attendance
GET    /api/meets/{group_id}/attendance/stream?meet_id={meet_id}&meet_id={meet_id}
PUT    /api/meet/{meet_id}/attendance/{venue_id}
DELETE /api/meet/{meet_id}/attendance/{venue_id}
```

Snapshot and stream reads require authentication and the same Meet/Group
visibility checks as voting. The Group stream requires between one and 50
distinct `meet_id` query values, all belonging to the requested Group. It sends
an `attendance_snapshots` event containing complete replacement snapshots for
all listed Meets. Each snapshot contains explicit `can_come` and `cannot_come`
UID arrays and applicable ETA values per Venue, the separate `any` entry,
public participant summaries, and a per-Meet cursor. The Group event also has a
Group cursor. Reconnects return the latest complete view; they do not replay
every intermediate write. Meet, attendance and public-profile changes refresh
the view. Attendance identities are not hidden by the voting-visibility
preference; private profile fields are never returned. Authorization changes
send a cursorless `view_reset` event and close the stream. Unavailable reads
return an error rather than an empty snapshot.

Both operations require a Firebase ID token and a `true` membership entry for
the Meet's Group. Public Group visibility and global administrator status do
not substitute for membership, and no voting permission is required. The
verified token UID is the only identity used; requests cannot name another user.

`PUT` accepts exactly one JSON field, `status`, with value `attending` or
`not_attending`, and returns `204 No Content`. `DELETE` withdraws the caller's
response and also returns `204`. Both are idempotent. The Meet and attendance
document are read and changed transactionally. Status arrays remain mutually
exclusive, and switching to `not_attending` or withdrawing also removes only
that caller's ETA. The literal `any` path segment sets or withdraws an
independent Any-Venue response in `attendance/{meet_id}.any`; it never copies
the response into Venue entries or overwrites them. A standing response is the
fallback for main Venue candidates only. Explicit Venue responses take
precedence, and restaurant attendance never inherits `any`. If one Venue serves
both roles, its single explicit Venue response is used for both.

Changing or withdrawing `any` also clears, in the same transaction, ETAs that
no longer have effective can-come attendance. ETAs for explicit Venue can-come
responses are retained. Existing Venue entries, including legacy copies whose
origin cannot be distinguished, are preserved and treated as explicit
responses. Older clients may continue reading and writing the existing
attendance document shape; those writes remain visible through the API snapshot
and Group stream.

An open Meet accepts its candidate Venues. A completed Meet accepts its selected
Venue and restaurant. A removed Venue can be withdrawn when the caller has a
response for it or the Meet records its removal; an unrelated Venue is rejected.
The literal `any` is the independent standing-response target, not a Venue.

## Attendance Statistics

```http
GET /api/attendance/statistics?start=YYYY-MM-DD&end=YYYY-MM-DD
```

The historical report requires a Firebase ID token and the global `admin` role.
`start` and `end` are inclusive, validated ISO dates, and `start` must not be
after `end`. The response is a JSON aggregate of completed Meets across every
Group, with `counts_by_venue_id` and `last_date_by_venue_id`. A Venue is included
for its candidate, historical, selected or restaurant references. Main Venues
use effective can-come responses: explicit Venue exceptions override the
separate `any` standing response. Restaurants use only explicit responses and
never inherit `any`. Source or aggregation failures return `503`; they are never
returned as zero attendance.
`PATCH /api/meet/{meet_id}/attendance/{venue_id}` independently updates the
caller's status and ETA. Omitting `eta` preserves it; `eta: null` removes it.
The React interface uses this API for status, standing-response, ETA and
withdrawal actions, and refreshes the authoritative snapshot after uncertain
writes. The backend watch keeps compatible legacy writes visible through the
same snapshot and stream. Firestore rules remain unchanged during the
compatibility window.

## Compatibility and Rollout

Roll out the backend first, then the frontend. Do not restrict direct
attendance access during the backend or frontend releases: the new API and
existing Firestore document shape must operate together while old clients
remain in use.

Keep the existing Firestore attendance rules unchanged for one week after the
frontend rollout. During that compatibility window, old clients retain their
prior all-Venues copying and restaurant-fallback behaviour. Existing and new
writes remain mutually visible: legacy records remain in their existing
Venue-keyed shape, copied and deliberate legacy entries cannot be distinguished
reliably, and the backend preserves those entries rather than reconstructing
provenance or reconfirming clients.

After one week, the client rules may be tightened in a separately reviewed and
authorised operational change so direct browser attendance access is denied and
the API becomes authoritative. This rollout document records the sequence only;
there is no deployment, production data migration or rules change in application
implementation. Backend administrative access remains separate and available.

Preferences offers **Email Status**, with a modal showing history and a manual
refresh command. Closing, navigation, sign-out and account changes cancel pending
requests; the ten-second frontend deadline includes token acquisition. States are
the durable backend's current observations, not an independent delivery probe.

Focused verification includes:

```powershell
go test ./internal/lastorders/endpoints/emailhistory
go test ./internal/lastorders/app -run TestAuthenticatedEmailHistoryEndToEnd
```

## Configuration

| Setting | Behaviour |
| --- | --- |
| `VITE_LAST_ORDERS_API_BASE_URL` | Frontend build-time URL; defaults to `http://localhost:8081` |
| `FIREBASE_AUTH_PROJECT_ID` | Explicit Auth project; must match `VITE_FIREBASE_PROJECT_ID` |
| `GOOGLE_CLOUD_PROJECT` | Explicit Firestore project and CLI fallback for Auth project, before credential-based detection |
| `LAST_ORDERS_FIREBASE_CREDENTIALS_JSON` | Complete service-account JSON as a backend runtime secret; shared by Firestore and Firebase Auth; never a frontend variable |
| `GOOGLE_APPLICATION_CREDENTIALS` | Backend credential file path; mutually exclusive with the JSON setting; never a frontend variable |
| `LAST_ORDERS_ALLOWED_ORIGINS` | Comma-separated exact frontend origins; defaults to localhost/127.0.0.1 port 3000 |
| `LAST_ORDERS_ALLOWED_PREVIEW_SITES` | Optional comma-separated Firebase Hosting site IDs whose HTTPS preview origins are allowed; disabled by default |
| `FIREBASE_AUTH_EMULATOR_HOST` | Local Auth emulator `host:port`, independent of Firestore emulator configuration |
| `-allow-auth-emulator` | Explicit development-only opt-in for unsigned local emulator tokens; default false |
| `-http-addr` | HTTP listen address; defaults to `:8080`; empty disables HTTP and Auth initialisation |

When `LAST_ORDERS_FIREBASE_CREDENTIALS_JSON` is set, an empty or invalid value
rejects startup without falling back to other credentials. Supply raw JSON, not a
path or base64 value. Standard JSON `\n` escapes in `private_key` are supported.
`FIREBASE_AUTH_PROJECT_ID` takes precedence over `GOOGLE_CLOUD_PROJECT`; otherwise
Auth uses the project from the supplied credentials (or the existing emulator
default when Firestore emulator mode is selected). With the JSON setting unset,
Google's Application Default Credentials behaviour is unchanged. See the README's
Docker deployment guidance for environment-only secret configuration.

An explicitly empty exact-origin allowlist permits no exact origins; configured
preview sites can still permit their previews. With both lists empty, no browser
origins are permitted. CORS is scoped
to `/api/`; `/log` is unchanged. Allowed preflight requests require no token and
allow `POST`, `Authorization` and `Content-Type`. Actual API calls always require
a token. CORS is not authentication and does not restrict non-browser clients.

Outside loopback development, frontend URLs and allowed origins require HTTPS.
Production TLS may terminate at a reverse proxy. API redirects are refused, and
browser cookies are not sent. Set the backend URL before the frontend build;
changing it requires rebuilding. A localhost default in a deployed bundle points
at the end user's machine, not the deployed backend.

The live and pull-request Firebase Hosting workflows read the public repository
Actions variable `VITE_LAST_ORDERS_API_BASE_URL` into the frontend build. Set that
variable to the deployed backend HTTPS URL. Preview origins can be added exactly
or enabled for specific Firebase Hosting sites as described below; arbitrary
wildcard origins are not supported.

### Firebase Hosting Preview Origins

For the permanent sites and temporary previews of `pubnightpicker`, add these
environment variables to the existing backend Docker service:

```yaml
environment:
	LAST_ORDERS_ALLOWED_ORIGINS: "https://ampubnight.org,https://pubnightpicker.web.app"
	LAST_ORDERS_ALLOWED_PREVIEW_SITES: "pubnightpicker"
```

Recreate or restart the container with the updated environment. These settings
are read at process startup, not dynamically. The preview setting takes site IDs,
not URLs, regular expressions or wildcards. It is disabled when absent or empty.

`pubnightpicker` allows valid HTTPS origins of the form
`https://pubnightpicker--<preview-label>.web.app`, including
`https://pubnightpicker--pr141-bug-test-build-ie5jywxw.web.app`. Matching requires a
valid single DNS label, exact site prefix and `.web.app` suffix. Other sites,
additional subdomains, HTTP, explicit ports, user information, paths, queries and
fragments are rejected. Permanent `pubnightpicker.web.app` remains an exact-origin
entry; the preview setting does not enable it implicitly.

Allowing previews only passes the browser-origin check. Firebase authentication
remains required, and future endpoints must enforce their own authorisation.
Preview code can call this backend as its signed-in users, including production
data operations if those are introduced. Enable only sites whose preview builds
you trust; CORS is not a replacement for authentication or authorisation.

Caddy needs no preview-specific change: all frontends use the same configured
backend HTTPS URL, Caddy forwards their `Origin` and `Authorization` headers, and
Go handles preflight and CORS. Do not add conflicting CORS headers at the proxy.

## Local Development

Start Firebase Auth and Firestore emulators from `react`, using the same project
ID as the frontend configuration:

```powershell
npx firebase-tools emulators:start --only auth,firestore --project pubnightpicker
```

In a separate terminal under `last_orders`:

```powershell
$env:GOOGLE_CLOUD_PROJECT = "pubnightpicker"
$env:FIREBASE_AUTH_PROJECT_ID = "pubnightpicker"
$env:FIRESTORE_EMULATOR_HOST = "127.0.0.1:8080"
$env:FIREBASE_AUTH_EMULATOR_HOST = "127.0.0.1:9099"
go run ./cmd/last-orders '-http-addr=127.0.0.1:8081' -allow-auth-emulator
```

Replace `pubnightpicker` if the frontend uses a different project. Run Vite on
port 3000, sign in with an emulator email/password account having the existing
admin role, open diagnostics and press **Ping Last Orders**. The response and log
must show the same verified UID. DevTools shows an HTTP preflight and POST to Go,
not a Firestore request/acknowledgement handshake. Other diagnostics can still
query Firestore independently. The frontend defaults to emulators on localhost.

Port 8081 avoids the Firestore emulator's 8080. The backend's existing default
HTTP port and Docker exposed port remain unchanged.

The Auth emulator setting without the opt-in flag causes startup to fail.
Only loopback emulator hosts are accepted. Never set the emulator variable or
opt-in flag in production; emulator tokens are unsigned and are not secure
production identities.

The SDK's emulator verification also looks up the emulator user. Use a token
issued by a real emulator sign-in, not a fabricated UID. This local Auth lookup
does not involve Firestore.

## Verification

Focused Go checks, from `last_orders`:

```powershell
go test ./internal/lastorders/components/firebaseauth ./internal/lastorders/components/apicors ./internal/lastorders/endpoints/ping
go test ./internal/lastorders/app -run 'Test.*(Ping|HTTPAuth|HTTPInvalidAPIOrigin|LogEndpoint)'
go test ./...
```

An integration test creates and removes a temporary anonymous Auth emulator
account and sends its actual ID token through the SDK and ping handler. With the
Auth emulator running and the two Auth environment variables above set:

```powershell
go test ./internal/lastorders/components/firebaseauth -run TestFirebaseAuthEmulatorPing -v
```

Frontend tests, from `react`:

```powershell
npm run test -- --run src/components/UI/BackendPingPanel.test.js src/components/pages/DiagnosticsPage.test.js
npm run typecheck
npm run build
```

The frontend suite's shared global setup starts a Firestore test emulator even
for mocked HTTP tests, so its existing Firebase CLI/Java prerequisites apply.
The ping tests themselves do not require Firestore.

The UI does not auto-ping or automatically retry. Its ten-second deadline includes
token acquisition, HTTP and response parsing. Sign-out, account changes and
navigation cancel pending work; late results cannot send or overwrite the UI.
Browsers cannot reliably distinguish CORS failure from a network error, so the
connection message covers both.

Before production rollout, verify a real matching-project token succeeds, missing
and altered tokens fail, wrong-project tokens fail, the HTTPS origin is allowed,
and emulator acceptance is disabled. Provisioning hosting/TLS and adding future
logging operations are outside this feature.
