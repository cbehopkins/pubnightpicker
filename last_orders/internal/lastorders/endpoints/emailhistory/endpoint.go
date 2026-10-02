package emailhistory

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	durableemail "durable_email"
	"last_orders/internal/lastorders/components/apirequest"
	"last_orders/internal/lastorders/components/firebaseauth"
)

type ProgressReader interface {
	QueryProgress(context.Context, durableemail.ProgressFilter) ([]durableemail.ProgressRow, error)
}

type Endpoint struct {
	Store  ProgressReader
	Logger *slog.Logger
}

type entry struct {
	Subject     string     `json:"subject"`
	Recipient   string     `json:"recipient"`
	State       string     `json:"state"`
	CreatedAt   *time.Time `json:"created_at"`
	SubmittedAt *time.Time `json:"submitted_at"`
}

func (endpoint *Endpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	principal, ok := firebaseauth.FromContext(r.Context())
	if !ok || principal.UID == "" {
		firebaseauth.WriteError(w, http.StatusUnauthorized, "unauthenticated", "A valid sign-in token is required.")
		return
	}
	requestID, ok := apirequest.ReadRequestID(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	rows, err := endpoint.Store.QueryProgress(ctx, durableemail.ProgressFilter{UserID: principal.UID, LatestFirst: true, Limit: 20})
	logger := endpoint.Logger
	if logger == nil {
		logger = slog.Default()
	}
	if err != nil {
		logger.ErrorContext(ctx, "email history query failed", "uid", principal.UID, "request_id", requestID, "error", err)
		firebaseauth.WriteError(w, http.StatusServiceUnavailable, "history_unavailable", "Email history is temporarily unavailable.")
		return
	}
	entries := make([]entry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, entry{Subject: row.Subject, Recipient: row.Recipient, State: row.State, CreatedAt: row.CreatedAt, SubmittedAt: row.SubmittedAt})
	}
	logger.InfoContext(ctx, "authenticated email history query", "uid", principal.UID, "request_id", requestID, "count", len(entries))
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(struct {
		Status    string  `json:"status"`
		UID       string  `json:"uid"`
		RequestID string  `json:"request_id"`
		Entries   []entry `json:"entries"`
	}{"ok", principal.UID, requestID, entries})
}
