# CDD: Diagnostics Test Email

**Status:** Accepted
**Date:** 2026-10-01

---

## 1. Purpose

An administrator can ask, from the Diagnostics page, for a test email to be sent to their own account. This confirms end to end that notification emails reach them.

The Python `firebase_sub` backend implements the same contract; both backends may run against the same Firestore data.

---

## 2. Firestore Contract

The request and its acknowledgement live on the user's private document, `users/{uid}`:

| Field          | Writer   | Meaning                                                  |
|----------------|----------|----------------------------------------------------------|
| `testEmailReq` | client   | A fresh UUID for each request.                           |
| `testEmailAck` | backend  | A copy of `testEmailReq` once the request has been handled. |

The Firestore rules forbid clients from writing `testEmailAck`.

A request is pending when `testEmailReq` is a non-empty string which differs from `testEmailAck`.

The frontend treats the request as sent only when `testEmailAck` equals the UUID it wrote during the current session.

---

## 3. Listener

The `testemail` listener watches `users` where `testEmailReq != ""`. Firestore omits documents without the field, so only users who have ever made a request are observed.

For each added or modified document the listener:

1. ignores it unless a request is pending (§2);
2. resolves the recipient as `notificationEmail`, falling back to `email` (both trimmed; the result may be empty);
3. emits a `TestEmailRequested` Truth carrying `UserID`, `RequestID` and `Email` through the Firebase idempotency layer (CDD-0001), with identity `TestEmailRequested` / `{uid}:{requestId}`.

The acknowledgement check is an optimisation. The idempotency layer is the gate which ensures a given request is emitted at most once, including across restarts and repeated snapshots caused by the backend's own acknowledgement write.

---

## 4. Handling

`testemail.send` handles the Truth:

* **No address:** a Sequence containing only the acknowledgement step, so the request is not observed as pending indefinitely.
* **Rate limited:** the request is dropped. Nothing is sent and no acknowledgement is written; the frontend times out. This matches the Python backend.
* **Otherwise:** a Sequence of the durable email Send (CDD for `durable_email`) with idempotency token `test-email:{uid}:{requestId}`, followed by the acknowledgement step.

`testemail.acked` merges `testEmailAck = requestId` into `users/{uid}`.

Because the durable email Post step only completes once the provider has accepted the message, the acknowledgement is written only after a successful submission.

Dummy versus live delivery follows the email plugin's configured client.

---

## 5. Rate Limiting

Test emails consume tokens from a dedicated `email.test` token source (CDD-0010), separate from poll notification emails, so diagnostics cannot exhaust the allowance for real notifications.

* Limit: `app.TestEmailDailyLimit` (10) per day, overridable via `Config.TestEmailTokens`.
* Reset: midnight in the application's timezone.
* State: in-memory (Version 0), so a restart restores the full allowance.

A token is acquired only when an email will actually be sent.
