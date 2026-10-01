# Poll Email Notifications — Implementation Plan

**Status:** Working plan
**Purpose:** Sequence the work needed to send durable emails for poll open,
poll completion, and poll reschedule from `last_orders`.

---

## Steps

| Step | Work | Depends on |
| ---- | ---- | ---------- |
| 1 | Project email recipient data into `notificationprofile` | — |
| 1b | Wait for the notification projection's first full load at startup | 1 |
| 2 | Send the poll-open email from the `PollOpened` Truth | 1, 1b |
| 3 | Carry the selected pub and poll date in the completed-poll Truth | — |
| 4 | Determine reschedule explicitly rather than from `ChangeKind` | 3 |
| 5 | Send completion and reschedule emails | 1, 1b, 3, 4 |

Steps 1, 1b, and 2 are planned in detail. Steps 3–5 will be planned once those
land.

---

## Step 1 — Email recipient data in the notification projection

Firestore `users` documents provide:

| Field | Meaning | Default |
| ----- | ------- | ------- |
| `notificationEmail` | Notification email address | empty |
| `openPollEmailEnabled` | Email when a poll opens | `false` |
| `notificationEmailEnabled` | Personal email when a poll completes | `false` |

A user receives email only when the relevant flag is exactly `true` and the
address is non-empty. Email eligibility is independent of `webPushEnabled`.

Changes:

* Add the three values to `UserPreferences`, `PreferencesFromDocument`, and
  `notification_user_prefs`.
* Add missing columns to existing databases with guarded
  `ALTER TABLE ... ADD COLUMN` statements.
* Add an email-kind recipient query, exposed through the notification profile
  service.
* Extend CDD 0009 to include email recipient data.

## Step 1b — Projection readiness

The notification profile listener exposes `Ready()`. It closes once, after the
users and endpoints streams have both applied their first batch. Reconnects do
not reopen it.

`App.Run`:

1. recovers durable email submissions;
2. starts the notification profile listener;
3. waits for `Ready()` or cancellation;
4. starts Cellar;
5. starts work-producing listeners.

There is no readiness timeout. If Firestore is unavailable at startup, the
application waits rather than processing work against an incomplete projection.

## Step 2 — Poll-open email

A `PollOpened` Truth handler selects users eligible for poll-open email and
creates one batched durable email Send.

* Idempotency token: `poll-opened:<pollID>`.
* Sender: `ampubnight notification emails <ampubnight@contable.co.uk>`.
* Subject: `Pub Night voting opened`.
* Body:

  ```text
  Voting has opened for this week's pub night.
  Please visit https://pubnightpicker.web.app/active_polls
  to participate in the voting.
  ```

The Send is returned in `cellar.Complete.NewCells`, so it commits atomically
with the Truth handler. No Send is created when there are no recipients.

Not included: rate limiting and configurable sender details.

## Step 3 — Completed-poll Truth data

The completed-poll Truth carries the selected pub ID and poll date. The
listener reads `restaurant`, matching the frontend and Python service. Venue
details are resolved from `venuecache`.

## Step 4 — Reschedule detection

Use Python's `comp_actions/{pollId}` history in Firestore, so it survives loss
of the local SQLite database. Each action type stores an array of Python
`complete_key` values. An action is needed when its key is absent; it is a
reschedule when that action type has previously been actioned for the poll.

## Step 5 — Completion and reschedule emails

Mailing-list and personal emails are separate Sends, recorded as `email` and
`pemail`. Content uses provider variables; personal messages carry `uid` as a
recipient variable for the unsubscribe link. The `comp_actions` marker is a
final Cellar step after durable `Post`, so it is written only after provider
acceptance. Startup waits for both notification-profile and venue-cache
readiness.
