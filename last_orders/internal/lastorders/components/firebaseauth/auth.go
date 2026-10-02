package firebaseauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

var ErrUnauthenticated = errors.New("unauthenticated")

type Principal struct {
	UID string
}

type Verifier interface {
	Verify(context.Context, string) (Principal, error)
}

type principalKey struct{}

func FromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalKey{}).(Principal)
	return principal, ok && principal.UID != ""
}

func Middleware(verifier Verifier, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		values := r.Header.Values("Authorization")
		if len(values) != 1 {
			WriteError(w, http.StatusUnauthorized, "unauthenticated", "A valid sign-in token is required.")
			return
		}
		parts := strings.Fields(values[0])
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || verifier == nil {
			WriteError(w, http.StatusUnauthorized, "unauthenticated", "A valid sign-in token is required.")
			return
		}
		verificationCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		principal, err := verifier.Verify(verificationCtx, parts[1])
		if errors.Is(err, ErrUnauthenticated) || (err == nil && principal.UID == "") {
			w.Header().Set("WWW-Authenticate", `Bearer realm="last_orders"`)
			WriteError(w, http.StatusUnauthorized, "unauthenticated", "The sign-in token was refused. Please sign in again.")
			return
		}
		if err != nil {
			WriteError(w, http.StatusServiceUnavailable, "auth_unavailable", "Authentication is temporarily unavailable.")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, principal)))
	})
}

func WriteError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", `Bearer realm="last_orders"`)
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}{Error: struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}{Code: code, Message: message}})
}
