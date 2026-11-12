// Package notificationprofile maintains a local projection of the Firebase notification
// data required to choose the recipients of a push notification.
//
// Firebase remains authoritative; this package owns the translation from Firebase
// documents into a notification-oriented local representation so the rest of the
// backend never depends on the Firebase schema. See docs/cdd/0009-notification-projection.md.
package notificationprofile

import (
	"fmt"
	"regexp"
	"slices"
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
	// KindDiagnostic is the admin push test. It ignores per-type preferences but,
	// like Python, still requires the webPushEnabled master switch.
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

	defaultOpenPollEmail            = false
	defaultNotificationEmailEnabled = false
)

// EmailKind identifies the email preference which gates a delivery.
type EmailKind string

const (
	EmailPollOpens     EmailKind = "pollOpens"
	EmailPollCompletes EmailKind = "pollCompletes"
)

// EmailRecipient is one user eligible to receive an email notification.
type EmailRecipient struct {
	UserID string
	Email  string
}

// UserPreferences is the projected notification preference state for one user.
type UserPreferences struct {
	UserID                string
	WebPushEnabled        bool
	PollOpens             bool
	PollCompletes         bool
	GlobalChat            bool
	EventChat             bool
	EventChatMutedPollIDs []string
	// Email preferences are independent of WebPushEnabled.
	NotificationEmail        string
	OpenPollEmailEnabled     bool
	NotificationEmailEnabled bool
	UpdatedAt                time.Time
}

type PrivateUserContribution struct {
	UserID                   string
	Email                    *string
	NotificationEmail        *string
	DefaultArrivalTime       *string
	TestEmailReq             *string
	TestEmailAck             *string
	WebPushEnabled           *bool
	PollOpens                *bool
	PollCompletes            *bool
	GlobalChat               *bool
	EventChat                *bool
	OpenPollEmailEnabled     *bool
	NotificationEmailEnabled *bool
	EventChatMutedPollIDs    []string
}

func (user PrivateUserContribution) Equal(other PrivateUserContribution) bool {
	return user.UserID == other.UserID &&
		equalOptional(user.Email, other.Email) &&
		equalOptional(user.NotificationEmail, other.NotificationEmail) &&
		equalOptional(user.DefaultArrivalTime, other.DefaultArrivalTime) &&
		equalOptional(user.TestEmailReq, other.TestEmailReq) &&
		equalOptional(user.TestEmailAck, other.TestEmailAck) &&
		equalOptional(user.WebPushEnabled, other.WebPushEnabled) &&
		equalOptional(user.PollOpens, other.PollOpens) &&
		equalOptional(user.PollCompletes, other.PollCompletes) &&
		equalOptional(user.GlobalChat, other.GlobalChat) &&
		equalOptional(user.EventChat, other.EventChat) &&
		equalOptional(user.OpenPollEmailEnabled, other.OpenPollEmailEnabled) &&
		equalOptional(user.NotificationEmailEnabled, other.NotificationEmailEnabled) &&
		equalStringSets(user.EventChatMutedPollIDs, other.EventChatMutedPollIDs)
}

func (user PrivateUserContribution) Preferences() UserPreferences {
	return UserPreferences{
		UserID:                   user.UserID,
		WebPushEnabled:           valueOr(user.WebPushEnabled, defaultWebPushEnabled),
		PollOpens:                valueOr(user.PollOpens, defaultPollOpens),
		PollCompletes:            valueOr(user.PollCompletes, defaultPollCompletes),
		GlobalChat:               valueOr(user.GlobalChat, defaultGlobalChat),
		EventChat:                valueOr(user.EventChat, defaultEventChat),
		EventChatMutedPollIDs:    normaliseStringSet(user.EventChatMutedPollIDs),
		NotificationEmail:        valueOr(user.NotificationEmail, ""),
		OpenPollEmailEnabled:     valueOr(user.OpenPollEmailEnabled, defaultOpenPollEmail),
		NotificationEmailEnabled: valueOr(user.NotificationEmailEnabled, defaultNotificationEmailEnabled),
	}
}

type NullableField[Value comparable] struct {
	Present bool
	Value   *Value
}

func (field NullableField[Value]) Equal(other NullableField[Value]) bool {
	return field.Present == other.Present && (!field.Present || equalOptional(field.Value, other.Value))
}

type PublicUserContribution struct {
	UserID         string
	Name           NullableField[string]
	PhotoURL       NullableField[string]
	CustomPhotoURL *string
	VotesVisible   NullableField[bool]
}

func (user PublicUserContribution) Equal(other PublicUserContribution) bool {
	return user.UserID == other.UserID && user.Name.Equal(other.Name) &&
		user.PhotoURL.Equal(other.PhotoURL) && user.VotesVisible.Equal(other.VotesVisible) &&
		equalOptional(user.CustomPhotoURL, other.CustomPhotoURL)
}

func PublicUserFromDocument(doc Document) (PublicUserContribution, error) {
	if doc.ID == "" {
		return PublicUserContribution{}, fmt.Errorf("user document ID is required")
	}
	user := PublicUserContribution{UserID: doc.ID}
	var err error
	user.Name, err = nullableValue[string](doc.Data, "name")
	if err != nil {
		return PublicUserContribution{}, err
	}
	user.PhotoURL, err = nullableValue[string](doc.Data, "photoUrl")
	if err != nil {
		return PublicUserContribution{}, err
	}
	user.VotesVisible, err = nullableValue[bool](doc.Data, "votesVisible")
	if err != nil {
		return PublicUserContribution{}, err
	}
	user.CustomPhotoURL, err = optionalValue[string](doc.Data, "customPhotoUrl")
	if err != nil {
		return PublicUserContribution{}, err
	}
	return user, nil
}

type EndpointContribution struct {
	EndpointID string
	UserID     string
	URL        string
	P256DH     string
	Auth       string
	Active     *bool
}

func (endpoint EndpointContribution) Equal(other EndpointContribution) bool {
	return endpoint.EndpointID == other.EndpointID && endpoint.UserID == other.UserID &&
		endpoint.URL == other.URL && endpoint.P256DH == other.P256DH &&
		endpoint.Auth == other.Auth && equalOptional(endpoint.Active, other.Active)
}

func (endpoint EndpointContribution) Endpoint() Endpoint {
	return Endpoint{
		EndpointID: endpoint.EndpointID, UserID: endpoint.UserID, URL: endpoint.URL,
		P256DH: endpoint.P256DH, Auth: endpoint.Auth,
		Active: valueOr(endpoint.Active, defaultEndpointActive),
	}
}

func EndpointContributionFromDocument(doc Document) (EndpointContribution, error) {
	if doc.ID == "" || doc.UserID == "" {
		return EndpointContribution{}, fmt.Errorf("endpoint document ID and owning user ID are required")
	}
	endpoint := EndpointContribution{EndpointID: doc.ID, UserID: doc.UserID}
	for _, field := range []struct {
		name   string
		target *string
	}{
		{"endpoint", &endpoint.URL}, {"p256dh", &endpoint.P256DH}, {"auth", &endpoint.Auth},
	} {
		value, err := requiredString(doc.Data, field.name)
		if err != nil {
			return EndpointContribution{}, err
		}
		*field.target = value
	}
	var err error
	endpoint.Active, err = optionalValue[bool](doc.Data, "active")
	if err != nil {
		return EndpointContribution{}, err
	}
	return endpoint, nil
}

var arrivalTimePattern = regexp.MustCompile(`^[0-2][0-9]:[0-5][0-9]$`)

func PrivateUserFromDocument(doc Document) (PrivateUserContribution, error) {
	if doc.ID == "" {
		return PrivateUserContribution{}, fmt.Errorf("user document ID is required")
	}
	user := PrivateUserContribution{UserID: doc.ID}
	for _, field := range []struct {
		name   string
		target **string
	}{
		{"email", &user.Email}, {"notificationEmail", &user.NotificationEmail},
		{"defaultArrivalTime", &user.DefaultArrivalTime}, {"testEmailReq", &user.TestEmailReq},
		{"testEmailAck", &user.TestEmailAck},
	} {
		value, err := optionalValue[string](doc.Data, field.name)
		if err != nil {
			return PrivateUserContribution{}, err
		}
		*field.target = value
	}
	if user.DefaultArrivalTime != nil && !arrivalTimePattern.MatchString(*user.DefaultArrivalTime) {
		return PrivateUserContribution{}, fmt.Errorf("invalid defaultArrivalTime %q", *user.DefaultArrivalTime)
	}
	preferences := map[string]any{}
	if raw, exists := doc.Data["pushPreferences"]; exists {
		var valid bool
		preferences, valid = raw.(map[string]any)
		if !valid || preferences == nil {
			return PrivateUserContribution{}, fmt.Errorf("pushPreferences must be an object")
		}
		for name := range preferences {
			if !slices.Contains([]string{"pollOpens", "pollCompletes", "globalChat", "eventChat", "eventChatMutedPollIds"}, name) {
				return PrivateUserContribution{}, fmt.Errorf("unknown pushPreferences field %q", name)
			}
		}
	}
	for _, field := range []struct {
		name   string
		data   map[string]any
		target **bool
	}{
		{"webPushEnabled", doc.Data, &user.WebPushEnabled},
		{"openPollEmailEnabled", doc.Data, &user.OpenPollEmailEnabled},
		{"notificationEmailEnabled", doc.Data, &user.NotificationEmailEnabled},
		{"pollOpens", preferences, &user.PollOpens},
		{"pollCompletes", preferences, &user.PollCompletes},
		{"globalChat", preferences, &user.GlobalChat},
		{"eventChat", preferences, &user.EventChat},
	} {
		value, err := optionalValue[bool](field.data, field.name)
		if err != nil {
			return PrivateUserContribution{}, err
		}
		*field.target = value
	}
	if raw, exists := preferences["eventChatMutedPollIds"]; exists {
		var ids []string
		switch values := raw.(type) {
		case []string:
			ids = slices.Clone(values)
		case []any:
			for _, value := range values {
				id, valid := value.(string)
				if !valid {
					return PrivateUserContribution{}, fmt.Errorf("eventChatMutedPollIds must contain only strings")
				}
				ids = append(ids, id)
			}
		default:
			return PrivateUserContribution{}, fmt.Errorf("eventChatMutedPollIds must be an array")
		}
		user.EventChatMutedPollIDs = normaliseStringSet(ids)
	}
	return user, nil
}

func optionalValue[Value any](data map[string]any, name string) (*Value, error) {
	raw, exists := data[name]
	if !exists {
		return nil, nil
	}
	value, valid := raw.(Value)
	if !valid {
		return nil, fmt.Errorf("field %q has invalid type %T", name, raw)
	}
	return &value, nil
}

func equalOptional[Value comparable](left, right *Value) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func nullableValue[Value comparable](data map[string]any, name string) (NullableField[Value], error) {
	raw, exists := data[name]
	if !exists {
		return NullableField[Value]{}, nil
	}
	if raw == nil {
		return NullableField[Value]{Present: true}, nil
	}
	value, err := optionalValue[Value](data, name)
	if err != nil {
		return NullableField[Value]{}, err
	}
	return NullableField[Value]{Present: true, Value: value}, nil
}

func valueOr[Value any](value *Value, fallback Value) Value {
	if value == nil {
		return fallback
	}
	return *value
}

func normaliseStringSet(values []string) []string {
	values = slices.Clone(values)
	slices.Sort(values)
	return slices.Compact(values)
}

func equalStringSets(left, right []string) bool {
	return slices.Equal(left, right) || slices.Equal(normaliseStringSet(left), normaliseStringSet(right))
}

// Enabled reports whether the user wants notifications of the given kind.
func (p UserPreferences) Enabled(kind Kind) bool {
	if !p.WebPushEnabled {
		return false
	}
	switch kind {
	case KindDiagnostic:
		return true
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

func (endpoint Endpoint) Equal(other Endpoint) bool {
	return endpoint.EndpointID == other.EndpointID && endpoint.UserID == other.UserID &&
		endpoint.URL == other.URL && endpoint.P256DH == other.P256DH &&
		endpoint.Auth == other.Auth && endpoint.Active == other.Active
}

func (preferences UserPreferences) Equal(other UserPreferences) bool {
	return preferences.UserID == other.UserID && preferences.WebPushEnabled == other.WebPushEnabled &&
		preferences.PollOpens == other.PollOpens && preferences.PollCompletes == other.PollCompletes &&
		preferences.GlobalChat == other.GlobalChat && preferences.EventChat == other.EventChat &&
		preferences.NotificationEmail == other.NotificationEmail &&
		preferences.OpenPollEmailEnabled == other.OpenPollEmailEnabled &&
		preferences.NotificationEmailEnabled == other.NotificationEmailEnabled &&
		equalStringSets(preferences.EventChatMutedPollIDs, other.EventChatMutedPollIDs)
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
	user, err := PrivateUserFromDocument(doc)
	if err != nil {
		return UserPreferences{}, err
	}
	return user.Preferences(), nil
}

// EndpointFromDocument translates a Firebase push_endpoints document.
func EndpointFromDocument(doc Document) (Endpoint, error) {
	endpoint, err := EndpointContributionFromDocument(doc)
	if err != nil {
		return Endpoint{}, err
	}
	return endpoint.Endpoint(), nil
}

func requiredString(data map[string]any, field string) (string, error) {
	value, ok := data[field].(string)
	if !ok || value == "" {
		return "", fmt.Errorf("endpoint field %q is required and must be a non-empty string", field)
	}
	return value, nil
}
