package ping

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"

	"github.com/google/uuid"
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
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		firebaseauth.WriteError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "An application/json request is required.")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1024))
	if err != nil {
		var sizeError *http.MaxBytesError
		if errors.As(err, &sizeError) {
			firebaseauth.WriteError(w, http.StatusRequestEntityTooLarge, "request_too_large", "The ping payload must not exceed 1 KiB.")
		} else {
			firebaseauth.WriteError(w, http.StatusBadRequest, "invalid_request", "The ping payload could not be read.")
		}
		return
	}
	var request struct {
		RequestID string `json:"request_id"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		firebaseauth.WriteError(w, http.StatusBadRequest, "invalid_request", "A JSON object containing only request_id is required.")
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		firebaseauth.WriteError(w, http.StatusBadRequest, "invalid_request", "Only one JSON object is allowed.")
		return
	}
	requestID, err := uuid.Parse(request.RequestID)
	if err != nil || requestID.String() != request.RequestID {
		firebaseauth.WriteError(w, http.StatusBadRequest, "invalid_request", "request_id must be a canonical UUID.")
		return
	}
	logger := endpoint.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.InfoContext(r.Context(), "authenticated API ping", "uid", principal.UID, "request_id", request.RequestID)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(struct {
		Status    string `json:"status"`
		UID       string `json:"uid"`
		RequestID string `json:"request_id"`
	}{Status: "ok", UID: principal.UID, RequestID: request.RequestID})
}
