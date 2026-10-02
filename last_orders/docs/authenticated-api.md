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
| `GOOGLE_CLOUD_PROJECT` | CLI fallback for Auth project, before SDK credential-based detection |
| `GOOGLE_APPLICATION_CREDENTIALS` | Backend service-account path for production; never a frontend variable |
| `LAST_ORDERS_ALLOWED_ORIGINS` | Comma-separated exact frontend origins; defaults to localhost/127.0.0.1 port 3000 |
| `LAST_ORDERS_ALLOWED_PREVIEW_SITES` | Optional comma-separated Firebase Hosting site IDs whose HTTPS preview origins are allowed; disabled by default |
| `FIREBASE_AUTH_EMULATOR_HOST` | Local Auth emulator `host:port`, independent of Firestore emulator configuration |
| `-allow-auth-emulator` | Explicit development-only opt-in for unsigned local emulator tokens; default false |
| `-http-addr` | HTTP listen address; defaults to `:8080`; empty disables HTTP and Auth initialisation |

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
