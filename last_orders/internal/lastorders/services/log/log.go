// Package log implements the hello-world log Service: a Cell which, once run,
// logs the message it was given. See docs/adr/0008-app-structure.md.
package log

import (
	"context"
	"log/slog"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/truths"
)

// HandlerLogMessage is the Cellar handler which performs the log Service's work.
const HandlerLogMessage cellar.HandlerName = "log.message"

// Handler logs payload.Message. It is the Service Cell for the log endpoint.
type Handler struct {
	Logger *slog.Logger
}

func (h Handler) Handle(ctx context.Context, payload truths.LogMessage) cellar.Result {
	_ = ctx
	if h.Logger != nil {
		h.Logger.Info("log message", "message", payload.Message)
	}
	return cellar.Complete{}
}
