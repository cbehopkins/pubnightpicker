package groups

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"last_orders/internal/lastorders/components/firebaseauth"
	"last_orders/internal/lastorders/components/groupcache"
)

type reader interface {
	ListPublic(context.Context, *string) ([]groupcache.Group, error)
}

type membershipWriter interface {
	EnsureMember(context.Context, string, string) error
}

type Endpoint struct {
	Reader     reader
	Auth       firebaseauth.Verifier
	Membership membershipWriter
}

type group struct {
	ID         groupcache.GroupID    `json:"id"`
	Name       string                `json:"name"`
	Visibility groupcache.Visibility `json:"visibility"`
}

func (e *Endpoint) Register(mux *http.ServeMux) {
	mux.Handle("GET /api/groups", firebaseauth.OptionalMiddleware(e.Auth, http.HandlerFunc(e.list)))
	mux.Handle("PUT /api/groups/{group_id}/members/{user_id}", firebaseauth.Middleware(e.Auth, http.HandlerFunc(e.join)))
}

func (e *Endpoint) join(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("group_id")
	if strings.Contains(groupID, "/") || groupID == "." || groupID == ".." {
		firebaseauth.WriteError(w, http.StatusBadRequest, "invalid_request", "The Group ID is invalid.")
		return
	}
	principal, ok := firebaseauth.FromContext(r.Context())
	if !ok || principal.UID != r.PathValue("user_id") {
		firebaseauth.WriteError(w, http.StatusForbidden, "forbidden", "You can only join a Group as yourself.")
		return
	}
	if e.Membership == nil {
		firebaseauth.WriteError(w, http.StatusServiceUnavailable, "membership_unavailable", "Group membership is temporarily unavailable.")
		return
	}
	err := e.Membership.EnsureMember(r.Context(), groupID, principal.UID)
	switch {
	case errors.Is(err, errGroupNotFound):
		firebaseauth.WriteError(w, http.StatusNotFound, "not_found", "Group not found.")
	case errors.Is(err, errPrivateGroup):
		firebaseauth.WriteError(w, http.StatusForbidden, "forbidden", "You do not have permission to join this Group.")
	case err != nil:
		firebaseauth.WriteError(w, http.StatusServiceUnavailable, "membership_unavailable", "Group membership is temporarily unavailable.")
	default:
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	}
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
