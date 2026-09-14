package truths

import "cellar/pkg/cellar"

// LogMessageFanout is the durable Cellar handler name which fans this Truth out
// to its registered handlers.
const LogMessageFanout cellar.HandlerName = "truths.log_message"

// LogMessageRegistry declares which handlers receive this Truth.
var LogMessageRegistry = NewRegistry[LogMessage](LogMessageFanout)

// LogMessage is the Truth emitted once idempotency has established that a log
// message may be delivered.
type LogMessage struct {
	Message string `json:"message"`
}
