package groups

import (
	"context"
	"encoding/json"
	"net/http"

	"last_orders/internal/lastorders/components/firebaseauth"
	"last_orders/internal/lastorders/components/groupcache"
)

type reader interface {
	ListPublic(context.Context, *string) ([]groupcache.Group, error)
}

type Endpoint struct {
	Reader reader
	Auth   firebaseauth.Verifier
}

type group struct {
	ID         groupcache.GroupID    `json:"id"`
	Name       string                `json:"name"`
	Visibility groupcache.Visibility `json:"visibility"`
}

func (e *Endpoint) Register(mux *http.ServeMux) {
	mux.Handle("GET /api/groups", firebaseauth.OptionalMiddleware(e.Auth, http.HandlerFunc(e.list)))
}

func (e *Endpoint) list(w http.ResponseWriter, r *http.Request) {
	var exactName *string
	if names, exists := r.URL.Query()["name"]; exists {
		name := ""
		if len(names) > 0 {
			name = names[0]
		}
		exactName = &name
	}
	groups, err := e.Reader.ListPublic(r.Context(), exactName)
	if err != nil {
		firebaseauth.WriteError(w, http.StatusInternalServerError, "internal_error", "Unable to list Groups.")
		return
	}
	response := struct {
		Groups []group `json:"groups"`
	}{Groups: make([]group, 0, len(groups))}
	for _, item := range groups {
		response.Groups = append(response.Groups, group{ID: item.ID, Name: item.Name, Visibility: item.Visibility})
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(response)
}
