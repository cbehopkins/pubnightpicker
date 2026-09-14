// Package eventvenues observes event venues and publishes durable
// EventVenueObserved Truths.
//
// It watches event venues in Firestore and periodically re-evaluates the
// current venue set so that date-boundary changes are observed even when no
// document changes. Each observation contains the venue state and London
// calendar date, and is handed to the Firebase idempotency component before
// being stored as a Cellar cell.
//
// This package only captures observations. The recurrence plugin evaluates
// them and decides whether to repair a stale occurrence or create an event
// poll; the corresponding handlers perform that work.
package eventvenues
