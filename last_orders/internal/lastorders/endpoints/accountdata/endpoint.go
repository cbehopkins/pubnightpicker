package accountdata

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"last_orders/internal/lastorders/components/firebaseauth"
)

// Endpoint assembles a caller-scoped application data export from authoritative
// Firestore documents. Export fields are deliberately projected rather than
// copying persisted documents, which may contain secrets or other users' data.
type Endpoint struct{ Client *firestore.Client }

type export struct {
	ExportedAt time.Time         `json:"exported_at"`
	UserID     string            `json:"user_id"`
	Notice     exportNotice      `json:"notice"`
	Account    map[string]any    `json:"account"`
	Roles      []string          `json:"roles"`
	Groups     []groupEntry      `json:"groups"`
	Votes      []voteEntry       `json:"votes"`
	Attendance []attendanceEntry `json:"attendance_responses"`
	Messages   []messageEntry    `json:"chat_messages"`
	Audit      []auditEntry      `json:"administrative_activity"`
	Notices    []recordEntry     `json:"notification_activity"`
	Push       []pushEntry       `json:"notification_subscriptions"`
}

type exportNotice struct {
	Scope       string   `json:"scope"`
	Limitations []string `json:"limitations"`
	Omissions   []string `json:"omissions"`
}
type groupEntry struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}
type voteEntry struct {
	MeetID     string   `json:"meet_id"`
	VenueIDs   []string `json:"venue_ids"`
	MeetDate   string   `json:"meet_date,omitempty"`
	VenueNames []string `json:"venue_names,omitempty"`
}
type attendanceEntry struct {
	MeetID    string `json:"meet_id"`
	VenueID   string `json:"venue_id"`
	Status    string `json:"status"`
	ETA       any    `json:"estimated_arrival_time,omitempty"`
	MeetDate  string `json:"meet_date,omitempty"`
	VenueName string `json:"venue_name,omitempty"`
}
type messageEntry struct {
	ID        string `json:"id"`
	Text      string `json:"text"`
	SentAt    any    `json:"sent_at,omitempty"`
	ScopeType string `json:"scope_type,omitempty"`
	ScopeID   string `json:"scope_id,omitempty"`
}
type auditEntry struct {
	ID      string `json:"id"`
	Action  string `json:"action,omitempty"`
	Outcome string `json:"outcome,omitempty"`
	At      any    `json:"at,omitempty"`
	MeetID  string `json:"meet_id,omitempty"`
	VenueID string `json:"venue_id,omitempty"`
}
type recordEntry struct {
	Source string         `json:"source"`
	ID     string         `json:"id"`
	Events map[string]any `json:"events"`
}
type pushEntry struct {
	ID         string `json:"id"`
	CreatedAt  any    `json:"created_at,omitempty"`
	UpdatedAt  any    `json:"last_seen_at,omitempty"`
	DisabledAt any    `json:"disabled_at,omitempty"`
	Active     any    `json:"active,omitempty"`
	UserAgent  string `json:"user_agent,omitempty"`
	Platform   string `json:"platform,omitempty"`
	AppVersion string `json:"app_version,omitempty"`
}

// RegisterWithAuth is used by the app composition root, which owns the verifier.
func (e *Endpoint) RegisterWithAuth(mux *http.ServeMux, auth firebaseauth.Verifier) {
	mux.Handle("GET /api/account/export", firebaseauth.Middleware(auth, http.HandlerFunc(e.serve)))
}

func (e *Endpoint) serve(w http.ResponseWriter, r *http.Request) {
	p, ok := firebaseauth.FromContext(r.Context())
	if !ok {
		firebaseauth.WriteError(w, http.StatusUnauthorized, "unauthenticated", "A valid sign-in token is required.")
		return
	}
	if e.Client == nil {
		firebaseauth.WriteError(w, http.StatusServiceUnavailable, "export_unavailable", "The account export is temporarily unavailable.")
		return
	}
	result, err := e.build(r.Context(), p.UID)
	if err != nil {
		firebaseauth.WriteError(w, http.StatusServiceUnavailable, "export_unavailable", "The account export is temporarily unavailable.")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(result)
}

func (e *Endpoint) build(ctx context.Context, uid string) (export, error) {
	out := export{
		ExportedAt: time.Now().UTC(), UserID: uid,
		Notice: exportNotice{Scope: "retained application data reliably attributable to this account", Limitations: []string{"Records that cannot be reliably attributed to this account are excluded.", "Related Meet and Venue context is included only when available."}, Omissions: []string{"Authentication secrets and credentials", "Push subscription endpoint and authentication keys", "Unattributable notification records", "Email delivery history and events", "Provider and infrastructure logs", "Backups"}},
		Roles:  []string{}, Groups: []groupEntry{}, Votes: []voteEntry{}, Attendance: []attendanceEntry{}, Messages: []messageEntry{}, Audit: []auditEntry{}, Notices: []recordEntry{}, Push: []pushEntry{},
	}
	out.Account = map[string]any{}
	readDoc := func(collection, id string) (map[string]any, error) {
		d, err := e.Client.Collection(collection).Doc(id).Get(ctx)
		if status.Code(err) == codes.NotFound {
			return map[string]any{}, nil
		}
		if err != nil {
			return nil, err
		}
		return d.Data(), nil
	}
	private, err := readDoc("users", uid)
	if err != nil {
		return out, err
	}
	public, err := readDoc("user-public", uid)
	if err != nil {
		return out, err
	}
	for _, key := range []string{
		"email", "authProvider", "notificationEmail", "notificationEmailEnabled", "openPollEmailEnabled",
		"webPushEnabled", "defaultArrivalTime",
	} {
		if v, ok := private[key]; ok {
			out.Account[key] = v
		}
	}
	if preferences, ok := private["pushPreferences"].(map[string]any); ok {
		allowed := make(map[string]any)
		for _, key := range []string{"pollOpens", "pollCompletes", "globalChat", "eventChat", "eventChatMutedPollIds"} {
			if value, exists := preferences[key]; exists {
				allowed[key] = value
			}
		}
		out.Account["pushPreferences"] = allowed
	}
	for _, key := range []string{"name", "photoUrl", "customPhotoUrl", "votesVisible"} {
		if v, ok := public[key]; ok {
			out.Account[key] = v
		}
	}

	roleDocs, err := e.Client.Collection("roles").Documents(ctx).GetAll()
	if err != nil {
		return out, err
	}
	for _, d := range roleDocs {
		if d.Data()[uid] == true {
			out.Roles = append(out.Roles, d.Ref.ID)
		}
	}
	globalAdmin := false
	for _, role := range out.Roles {
		if role == "admin" {
			globalAdmin = true
			break
		}
	}
	groupDocs, err := e.Client.Collection("groups").Documents(ctx).GetAll()
	if err != nil {
		return out, err
	}
	accessibleGroups := make(map[string]bool, len(groupDocs))
	for _, d := range groupDocs {
		data := d.Data()
		members, _ := data["members"].(map[string]any)
		visibility, _ := data["visibility"].(string)
		accessibleGroups[d.Ref.ID] = globalAdmin || visibility == "public" || members[uid] == true
		if members[uid] == true {
			g := groupEntry{ID: d.Ref.ID}
			g.Name, _ = data["name"].(string)
			out.Groups = append(out.Groups, g)
		}
	}

	meets, err := e.Client.Collection("polls").Documents(ctx).GetAll()
	meetData := make(map[string]map[string]any, len(meets))
	if err == nil {
		for _, d := range meets {
			meetData[d.Ref.ID] = d.Data()
		}
	}
	venues, err := e.Client.Collection("pubs").Documents(ctx).GetAll()
	venueNames := make(map[string]string, len(venues))
	if err == nil {
		for _, d := range venues {
			if name, ok := d.Data()["name"].(string); ok {
				venueNames[d.Ref.ID] = name
			}
		}
	}
	voteDocs, err := e.Client.Collection("votes").Documents(ctx).GetAll()
	if err != nil {
		return out, err
	}
	for _, d := range voteDocs {
		data := d.Data()
		ids := []string{}
		for venueID, raw := range data {
			voters, ok := raw.([]any)
			if !ok {
				continue
			}
			for _, v := range voters {
				if v == uid {
					ids = append(ids, venueID)
					break
				}
			}
		}
		if len(ids) == 0 {
			continue
		}
		entry := voteEntry{MeetID: d.Ref.ID, VenueIDs: ids}
		if meetContextAllowed(meetData[d.Ref.ID], accessibleGroups) {
			if date, ok := meetData[d.Ref.ID]["date"].(string); ok {
				entry.MeetDate = date
			}
		}
		for _, id := range ids {
			if meetContextAllowed(meetData[d.Ref.ID], accessibleGroups) {
				if n := venueName(id, meetData[d.Ref.ID], venueNames); n != "" {
					entry.VenueNames = append(entry.VenueNames, n)
				}
			}
		}
		out.Votes = append(out.Votes, entry)
	}
	attendanceDocs, err := e.Client.Collection("attendance").Documents(ctx).GetAll()
	if err != nil {
		return out, err
	}
	for _, d := range attendanceDocs {
		for venueID, raw := range d.Data() {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			for _, field := range []struct{ name, status string }{{"canCome", "attending"}, {"cannotCome", "not_attending"}} {
				users, _ := item[field.name].([]any)
				for _, v := range users {
					if v != uid {
						continue
					}
					a := attendanceEntry{MeetID: d.Ref.ID, VenueID: venueID, Status: field.status}
					etas, _ := item["eta"].(map[string]any)
					a.ETA = etas[uid]
					if meetContextAllowed(meetData[d.Ref.ID], accessibleGroups) {
						if date, ok := meetData[d.Ref.ID]["date"].(string); ok {
							a.MeetDate = date
						}
						a.VenueName = venueName(venueID, meetData[d.Ref.ID], venueNames)
					}
					out.Attendance = append(out.Attendance, a)
				}
			}
		}
	}
	messages, err := e.Client.Collection("messages").Where("uid", "==", uid).Documents(ctx).GetAll()
	if err != nil {
		return out, err
	}
	for _, d := range messages {
		data := d.Data()
		m := messageEntry{ID: d.Ref.ID}
		m.Text, _ = data["text"].(string)
		m.SentAt = data["createdAt"]
		m.ScopeType, _ = data["scopeType"].(string)
		m.ScopeID, _ = data["scopeId"].(string)
		out.Messages = append(out.Messages, m)
	}
	audits, err := e.Client.Collection("poll_action_audit").Where("actorUid", "==", uid).Documents(ctx).GetAll()
	if err != nil {
		return out, err
	}
	for _, d := range audits {
		data := d.Data()
		a := auditEntry{ID: d.Ref.ID, At: data["at"]}
		a.Action, _ = data["actionType"].(string)
		a.MeetID, _ = data["pollId"].(string)
		a.VenueID, _ = data["selectedVenueId"].(string)
		out.Audit = append(out.Audit, a)
	}
	deleteAudits, err := e.Client.Collection("admin_delete_request_audit").Where("requestedByUid", "==", uid).Documents(ctx).GetAll()
	if err != nil {
		return out, err
	}
	for _, d := range deleteAudits {
		data := d.Data()
		a := auditEntry{ID: d.Ref.ID, Action: "account_deletion_request", At: data["at"]}
		a.Outcome, _ = data["outcome"].(string)
		out.Audit = append(out.Audit, a)
	}
	deleteRequests, err := e.Client.Collection("admin_delete_requests").Where("requestedByUid", "==", uid).Documents(ctx).GetAll()
	if err != nil {
		return out, err
	}
	for _, d := range deleteRequests {
		data := d.Data()
		a := auditEntry{ID: d.Ref.ID, Action: "account_deletion_request", At: data["createdAt"]}
		a.Outcome, _ = data["status"].(string)
		out.Audit = append(out.Audit, a)
	}
	pushDocs, err := e.Client.Collection("users").Doc(uid).Collection("push_endpoints").Documents(ctx).GetAll()
	if err != nil {
		return out, err
	}
	for _, d := range pushDocs {
		data := d.Data()
		p := pushEntry{ID: d.Ref.ID, CreatedAt: data["createdAt"], UpdatedAt: data["lastSeenAt"], DisabledAt: data["disabledAt"], Active: data["active"]}
		p.UserAgent, _ = data["userAgent"].(string)
		p.Platform, _ = data["platform"].(string)
		p.AppVersion, _ = data["appVersion"].(string)
		out.Push = append(out.Push, p)
	}
	for _, collection := range []string{"notification_req", "notification_ack"} {
		docs, readErr := e.Client.Collection(collection).Documents(ctx).GetAll()
		if readErr != nil {
			return out, readErr
		}
		for _, d := range docs {
			data := d.Data()
			if d.Ref.ID == "push_test" {
				if value, exists := data[uid]; exists {
					out.Notices = append(out.Notices, recordEntry{Source: collection, ID: d.Ref.ID, Events: map[string]any{"push_test": value}})
				}
			}
		}
	}
	return out, nil
}

func meetContextAllowed(meet map[string]any, accessibleGroups map[string]bool) bool {
	groupID, ok := meet["groupId"].(string)
	return ok && groupID != "" && accessibleGroups[groupID]
}

func venueName(venueID string, meet map[string]any, publicVenueNames map[string]string) string {
	if name := publicVenueNames[venueID]; name != "" {
		return name
	}
	meetVenues, _ := meet["pubs"].(map[string]any)
	venue, _ := meetVenues[venueID].(map[string]any)
	name, _ := venue["name"].(string)
	return name
}
