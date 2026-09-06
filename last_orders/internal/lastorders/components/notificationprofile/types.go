// Package notificationprofile maintains a local projection of the Firebase notification
// data required to choose the recipients of a push notification.
//
// Firebase remains authoritative; this package owns the translation from Firebase
// documents into a notification-oriented local representation so the rest of the
// backend never depends on the Firebase schema. See docs/cdd/0009-notification-projection.md.
package notificationprofile

import (
	"fmt"
	"time"
)

const (
	usersCollection     = "users"
	endpointsCollection = "push_endpoints"
)

// Kind identifies the notification preference which gates a delivery.
type Kind string

const (
	KindPollOpens     Kind = "pollOpens"
	KindPollCompletes Kind = "pollCompletes"
	KindGlobalChat    Kind = "globalChat"
	KindEventChat     Kind = "eventChat"
	// KindDiagnostic is the admin push test. It is gated on endpoint activity alone.
	KindDiagnostic Kind = "diagnostic"
)

// Preference defaults applied when Firebase omits a field, matching the existing
// Python service's PUSH_PREFERENCE_DEFAULTS.
const (
	defaultWebPushEnabled = false
	defaultPollOpens      = true
	defaultPollCompletes  = true
	defaultGlobalChat     = false
	defaultEventChat      = false
	defaultEndpointActive = false
)

// UserPreferences is the projected notification preference state for one user.
type UserPreferences struct {
	UserID                string
	WebPushEnabled        bool
	PollOpens             bool
	PollCompletes         bool
	GlobalChat            bool
	EventChat             bool
	EventChatMutedPollIDs []string
	UpdatedAt             time.Time
}

// Enabled reports whether the user wants notifications of the given kind.
func (p UserPreferences) Enabled(kind Kind) bool {
	if kind == KindDiagnostic {
		return true
	}
	if !p.WebPushEnabled {
		return false
	}
	switch kind {
	case KindPollOpens:
		return p.PollOpens
	case KindPollCompletes:
		return p.PollCompletes
	case KindGlobalChat:
		return p.GlobalChat
	case KindEventChat:
		return p.EventChat
	default:
		return false
	}
}

// MutedFor reports whether the user has muted event chat for the given poll.
func (p UserPreferences) MutedFor(pollID string) bool {
	for _, muted := range p.EventChatMutedPollIDs {
		if muted == pollID {
			return true
		}
	}
	return false
}

// Endpoint is one projected Web Push subscription.
type Endpoint struct {
	EndpointID string
	UserID     string
	URL        string
	P256DH     string
	Auth       string
	Active     bool
	UpdatedAt  time.Time
}

// Key is a stable identity for the endpoint across users. Endpoint document IDs are
// only unique within a user's subcollection, so both parts are required.
func (e Endpoint) Key() string {
	return e.UserID + "/" + e.EndpointID
}

// Selector describes which endpoints a notification should be delivered to.
// An empty UserIDs selects every eligible user.
type Selector struct {
	Kind    Kind
	UserIDs []string
}

// Document is a source document paired with the identity the projection needs.
// UserID is the owning user for endpoint documents and empty for user documents.
type Document struct {
	ID     string
	UserID string
	Data   map[string]any
}

var (
	ErrNotFound    = fmt.Errorf("notification profile not found")
	ErrUnknownKind = fmt.Errorf("unknown notification kind")
)

// PreferencesFromDocument translates a Firebase users document.
func PreferencesFromDocument(doc Document) (UserPreferences, error) {
	if doc.ID == "" {
		return UserPreferences{}, fmt.Errorf("user document ID is required")
	}
	preferences, _ := doc.Data["pushPreferences"].(map[string]any)
	return UserPreferences{
		UserID:                doc.ID,
		WebPushEnabled:        optionalBool(doc.Data, "webPushEnabled", defaultWebPushEnabled),
		PollOpens:             optionalBool(preferences, string(KindPollOpens), defaultPollOpens),
		PollCompletes:         optionalBool(preferences, string(KindPollCompletes), defaultPollCompletes),
		GlobalChat:            optionalBool(preferences, string(KindGlobalChat), defaultGlobalChat),
		EventChat:             optionalBool(preferences, string(KindEventChat), defaultEventChat),
		EventChatMutedPollIDs: optionalStrings(preferences, "eventChatMutedPollIds"),
	}, nil
}

// EndpointFromDocument translates a Firebase push_endpoints document.
func EndpointFromDocument(doc Document) (Endpoint, error) {
	if doc.ID == "" {
		return Endpoint{}, fmt.Errorf("endpoint document ID is required")
	}
	if doc.UserID == "" {
		return Endpoint{}, fmt.Errorf("endpoint document %q has no owning user", doc.ID)
	}
	url, err := requiredString(doc.Data, "endpoint")
	if err != nil {
		return Endpoint{}, err
	}
	p256dh, err := requiredString(doc.Data, "p256dh")
	if err != nil {
		return Endpoint{}, err
	}
	auth, err := requiredString(doc.Data, "auth")
	if err != nil {
		return Endpoint{}, err
	}
	return Endpoint{
		EndpointID: doc.ID,
		UserID:     doc.UserID,
		URL:        url,
		P256DH:     p256dh,
		Auth:       auth,
		Active:     optionalBool(doc.Data, "active", defaultEndpointActive),
	}, nil
}

func requiredString(data map[string]any, field string) (string, error) {
	value, ok := data[field].(string)
	if !ok || value == "" {
		return "", fmt.Errorf("endpoint field %q is required and must be a non-empty string", field)
	}
	return value, nil
}

func optionalBool(data map[string]any, field string, fallback bool) bool {
	value, ok := data[field].(bool)
	if !ok {
		return fallback
	}
	return value
}

func optionalStrings(data map[string]any, field string) []string {
	raw, ok := data[field].([]any)
	if !ok {
		return nil
	}
	values := make([]string, 0, len(raw))
	for _, item := range raw {
		if value, ok := item.(string); ok && value != "" {
			values = append(values, value)
		}
	}
	return values
}
