package apirequest

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"last_orders/internal/lastorders/components/firebaseauth"

	"github.com/google/uuid"
)

func ReadRequestID(w http.ResponseWriter, r *http.Request) (string, bool) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		firebaseauth.WriteError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "An application/json request is required.")
		return "", false
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1024))
	if err != nil {
		var sizeError *http.MaxBytesError
		if errors.As(err, &sizeError) {
			firebaseauth.WriteError(w, http.StatusRequestEntityTooLarge, "request_too_large", "The API payload must not exceed 1 KiB.")
		} else {
			firebaseauth.WriteError(w, http.StatusBadRequest, "invalid_request", "The API payload could not be read.")
		}
		return "", false
	}
	var request struct {
		RequestID string `json:"request_id"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		firebaseauth.WriteError(w, http.StatusBadRequest, "invalid_request", "A JSON object containing only request_id is required.")
		return "", false
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		firebaseauth.WriteError(w, http.StatusBadRequest, "invalid_request", "Only one JSON object is allowed.")
		return "", false
	}
	requestID, err := uuid.Parse(request.RequestID)
	if err != nil || requestID.String() != request.RequestID {
		firebaseauth.WriteError(w, http.StatusBadRequest, "invalid_request", "request_id must be a canonical UUID.")
		return "", false
	}
	return request.RequestID, true
}
