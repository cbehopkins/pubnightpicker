package ping

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"last_orders/internal/lastorders/components/apirequest"
	"last_orders/internal/lastorders/components/firebaseauth"
)

type Endpoint struct {
	Logger *slog.Logger
}

func (endpoint *Endpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	principal, ok := firebaseauth.FromContext(r.Context())
	if !ok {
		firebaseauth.WriteError(w, http.StatusUnauthorized, "unauthenticated", "A valid sign-in token is required.")
		return
	}
	requestID, ok := apirequest.ReadRequestID(w, r)
	if !ok {
		return
	}
	logger := endpoint.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.InfoContext(r.Context(), "authenticated API ping", "uid", principal.UID, "request_id", requestID)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(struct {
		Status    string `json:"status"`
		UID       string `json:"uid"`
		RequestID string `json:"request_id"`
	}{Status: "ok", UID: principal.UID, RequestID: requestID})
}
