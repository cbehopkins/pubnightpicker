package accountcleanup

import (
	"context"
	"encoding/json"
	"net/http"

	"last_orders/internal/lastorders/components/firebaseauth"
	"last_orders/internal/lastorders/services/votecleanup"
)

type Result = votecleanup.Result

type Cleaner interface {
	RemoveUser(context.Context, string) (Result, error)
}

type Endpoint struct {
	Cleaner Cleaner
	Auth    firebaseauth.Verifier
}

func (e *Endpoint) Register(mux *http.ServeMux) {
	mux.Handle("POST /api/account/votes/cleanup", firebaseauth.Middleware(e.Auth, http.HandlerFunc(e.cleanup)))
}

func (e *Endpoint) cleanup(w http.ResponseWriter, r *http.Request) {
	principal, ok := firebaseauth.FromContext(r.Context())
	if !ok {
		firebaseauth.WriteError(w, http.StatusUnauthorized, "unauthenticated", "A valid sign-in token is required.")
		return
	}
	if e.Cleaner == nil {
		firebaseauth.WriteError(w, http.StatusServiceUnavailable, "cleanup_unavailable", "Vote cleanup is temporarily unavailable.")
		return
	}
	result, err := e.Cleaner.RemoveUser(r.Context(), principal.UID)
	if err != nil {
		firebaseauth.WriteError(w, http.StatusInternalServerError, "cleanup_failed", "Unable to remove the account's votes.")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(result)
}
