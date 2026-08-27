package firebaseidempotency

import (
	"context"
)

// Remote is the external Firebase idempotency surface used by the idempotency component.
//
// Production code must bind this to a real Firebase-backed implementation; there is
// deliberately no non-durable fallback (see internal/lastorders/app.New). Tests may
// bind this to the in-memory stand-in in the firebaseidempotencytest package.
type Remote interface {
	CreateKey(ctx context.Context, listener, eventKey string) (alreadyExists bool, err error)
	HasKey(ctx context.Context, listener, eventKey string) (bool, error)
}
