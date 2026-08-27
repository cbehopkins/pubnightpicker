// Package log implements the hello-world log Service: a Cell which, once run,
// logs the message it was given. See docs/adr/0008-app-structure.md.
package log

import (
	"context"
	"log/slog"

	"cellar/pkg/cellar"
)

// FactLogMessage is the Fact emitted once idempotency has established that a log
// message may be delivered.
const FactLogMessage = "LogMessage"

// HandlerLogMessage is the Cellar handler which performs the log Service's work.
const HandlerLogMessage cellar.HandlerName = "log.message"

// Payload is the Service's unit of work: the message to log.
type Payload struct {
	Message string `json:"message"`
}

// Handler logs Payload.Message. It is the Service Cell for the log endpoint.
type Handler struct {
	Logger *slog.Logger
}

func (h Handler) Handle(ctx context.Context, payload Payload) cellar.Result {
	_ = ctx
	if h.Logger != nil {
		h.Logger.Info("log message", "message", payload.Message)
	}
	return cellar.Complete{}
}
