// Package log implements the hello-world HTTP endpoint described in
// docs/adr/0008-app-structure.md: an externally accessible entry point which
// durably records the request as a Cell before returning.
package log

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/components/facts"
	"last_orders/internal/lastorders/components/firebaseidempotency"
	logsvc "last_orders/internal/lastorders/services/log"
)

// ListenerLog namespaces this endpoint's idempotency identities, mirroring how a
// database Listener names itself when building an event key.
const ListenerLog = "endpoints.log"

// CellAdder durably persists a Cell. *app.App satisfies this.
type CellAdder interface {
	AddCell(request cellar.CellRequest) error
}

// Endpoint is the HTTP entry point for the log Service.
type Endpoint struct {
	Cells  CellAdder
	Logger *slog.Logger
}

type requestBody struct {
	EventID string `json:"event_id"`
	Message string `json:"message"`
}

func (e *Endpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body requestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if body.EventID == "" {
		http.Error(w, "event_id is required", http.StatusBadRequest)
		return
	}
	if body.Message == "" {
		http.Error(w, "message is required", http.StatusBadRequest)
		return
	}

	payload, err := cellar.JSONCodec[logsvc.Payload]().Marshal(logsvc.Payload{Message: body.Message})
	if err != nil {
		e.logError("marshal log payload", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	request, err := firebaseidempotency.NewCellRequest(ListenerLog, body.EventID, facts.Fact{Name: logsvc.FactLogMessage, Payload: payload})
	if err != nil {
		e.logError("build log cell request", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if err := e.Cells.AddCell(request); err != nil {
		e.logError("add log cell", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(`{"status":"accepted"}`))
}

func (e *Endpoint) logError(msg string, err error) {
	if e.Logger != nil {
		e.Logger.Error(msg, "err", err)
	}
}
