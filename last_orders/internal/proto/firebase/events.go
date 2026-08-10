package firebase

import "time"

const (
	EventPollCreated   = "poll_created"
	EventPollModified  = "poll_modified"
	EventPollCompleted = "poll_completed"
	EventPollDeleted   = "poll_deleted"
)

// Event is the minimal payload the prototype needs from Firebase listeners.
type Event struct {
	Type        string
	PollID      string
	ObservedAt  time.Time
	DeliveryKey string
	Raw         map[string]any
}

// Source produces Firebase events from a concrete backing implementation.
type Source interface {
	Run(ctxDone <-chan struct{}, out chan<- Event) error
}
