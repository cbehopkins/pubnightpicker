package notificationprofile

import (
	"slices"
	"testing"
	"time"
)

func TestPrivateUserContributionRetainsFieldsAndNormalisesIDs(t *testing.T) {
	ids := []string{"poll-2", "", "poll-1", "poll-2", " poll-1 "}
	doc := Document{ID: "user", Data: map[string]any{
		"uid": "ignored", "name": "ignored private name", "unknown": true,
		"email": "user@example.com", "notificationEmail": "", "defaultArrivalTime": "19:30",
		"testEmailReq": "request", "testEmailAck": "ack", "webPushEnabled": false,
		"openPollEmailEnabled": true, "notificationEmailEnabled": false,
		"pushPreferences": map[string]any{
			"pollOpens": false, "pollCompletes": true, "globalChat": false, "eventChat": true,
			"eventChatMutedPollIds": ids,
		},
	}}
	user, err := PrivateUserFromDocument(doc)
	if err != nil {
		t.Fatal(err)
	}
	email, empty, arrival, request, ack := "user@example.com", "", "19:30", "request", "ack"
	enabled, disabled := true, false
	want := PrivateUserContribution{
		UserID: "user", Email: &email, NotificationEmail: &empty, DefaultArrivalTime: &arrival,
		TestEmailReq: &request, TestEmailAck: &ack, WebPushEnabled: &disabled,
		OpenPollEmailEnabled: &enabled, NotificationEmailEnabled: &disabled,
		PollOpens: &disabled, PollCompletes: &enabled, GlobalChat: &disabled, EventChat: &enabled,
		EventChatMutedPollIDs: []string{"", " poll-1 ", "poll-1", "poll-2"},
	}
	if !user.Equal(want) || !slices.Equal(user.EventChatMutedPollIDs, want.EventChatMutedPollIDs) {
		t.Fatalf("private contribution = %+v; want %+v", user, want)
	}
	if !slices.Equal(ids, []string{"poll-2", "", "poll-1", "poll-2", " poll-1 "}) {
		t.Fatal("normalisation changed source IDs")
	}
	absent, err := PrivateUserFromDocument(Document{ID: "user"})
	if err != nil || !absent.Equal(PrivateUserContribution{UserID: "user"}) {
		t.Fatalf("missing fields acquired defaults: %+v, %v", absent, err)
	}
	if absent.Equal(user) {
		t.Fatal("missing and explicitly supplied values compared equal")
	}
}

func TestPrivateUserContributionRejectsInvalidValues(t *testing.T) {
	for _, data := range []map[string]any{
		{"email": nil}, {"notificationEmail": false}, {"defaultArrivalTime": "9:30"},
		{"testEmailReq": 1}, {"testEmailAck": nil}, {"webPushEnabled": "yes"},
		{"openPollEmailEnabled": nil}, {"notificationEmailEnabled": 1},
		{"pushPreferences": nil}, {"pushPreferences": "invalid"},
		{"pushPreferences": map[string]any{"unknown": true}},
		{"pushPreferences": map[string]any{"pollOpens": nil}},
		{"pushPreferences": map[string]any{"eventChatMutedPollIds": nil}},
		{"pushPreferences": map[string]any{"eventChatMutedPollIds": []any{"poll-1", 7}}},
	} {
		if _, err := PrivateUserFromDocument(Document{ID: "user", Data: data}); err == nil {
			t.Fatalf("accepted invalid user data: %#v", data)
		}
	}
	if _, err := PrivateUserFromDocument(Document{}); err == nil {
		t.Fatal("accepted missing user document ID")
	}
}

func TestPreferencesFromDocumentAppliesDefaults(t *testing.T) {
	preferences, err := PreferencesFromDocument(Document{ID: "user", Data: map[string]any{}})
	if err != nil {
		t.Fatalf("preferences from document: %v", err)
	}
	if preferences.WebPushEnabled {
		t.Error("web push should default to disabled")
	}
	if !preferences.PollOpens || !preferences.PollCompletes {
		t.Error("poll preferences should default to enabled")
	}
	if preferences.GlobalChat || preferences.EventChat {
		t.Error("chat preferences should default to disabled")
	}
	if preferences.NotificationEmail != "" || preferences.OpenPollEmailEnabled || preferences.NotificationEmailEnabled {
		t.Error("email preferences should default to no address and disabled")
	}
}

func TestPreferencesFromDocumentReadsEmailPreferences(t *testing.T) {
	preferences, err := PreferencesFromDocument(Document{ID: "user", Data: map[string]any{
		"notificationEmail":        "user@example.com",
		"openPollEmailEnabled":     true,
		"notificationEmailEnabled": false,
	}})
	if err != nil {
		t.Fatalf("preferences from document: %v", err)
	}
	if preferences.NotificationEmail != "user@example.com" || !preferences.OpenPollEmailEnabled {
		t.Fatalf("preferences = %+v; want the email fields applied", preferences)
	}
	if preferences.NotificationEmailEnabled {
		t.Error("an explicit false flag should remain disabled")
	}
	if preferences.Enabled(KindPollOpens) {
		t.Error("email preferences must not enable push")
	}
}

func TestPreferencesFromDocumentReadsNestedPreferences(t *testing.T) {
	preferences, err := PreferencesFromDocument(Document{ID: "user", Data: map[string]any{
		"webPushEnabled": true,
		"pushPreferences": map[string]any{
			"pollOpens":             false,
			"globalChat":            true,
			"eventChatMutedPollIds": []any{"poll-2", "", "poll-1", "poll-2"},
		},
	}})
	if err != nil {
		t.Fatalf("preferences from document: %v", err)
	}
	if !preferences.WebPushEnabled || preferences.PollOpens || !preferences.GlobalChat {
		t.Fatalf("preferences = %+v; want nested values applied", preferences)
	}
	if !preferences.PollCompletes {
		t.Error("unset pollCompletes should keep its default")
	}
	if !slices.Equal(preferences.EventChatMutedPollIDs, []string{"", "poll-1", "poll-2"}) {
		t.Fatalf("muted poll ids = %v; want a normalised set without rewriting IDs", preferences.EventChatMutedPollIDs)
	}
}

func TestPreferencesEnabledRespectsMasterSwitch(t *testing.T) {
	preferences := UserPreferences{WebPushEnabled: false, PollOpens: true}
	if preferences.Enabled(KindPollOpens) {
		t.Error("master switch off should disable poll opens")
	}
	if preferences.Enabled(KindDiagnostic) {
		t.Error("master switch off should disable diagnostic push")
	}
	if !(UserPreferences{WebPushEnabled: true}).Enabled(KindDiagnostic) {
		t.Error("diagnostic push should ignore per-type preferences")
	}
}

func TestPreferencesMutedFor(t *testing.T) {
	preferences := UserPreferences{EventChatMutedPollIDs: []string{"poll-1"}}
	if !preferences.MutedFor("poll-1") || preferences.MutedFor("poll-2") {
		t.Fatal("MutedFor did not match the muted poll list")
	}
}

func TestEndpointFromDocument(t *testing.T) {
	valid := map[string]any{
		"endpoint": "https://push.test/abc",
		"p256dh":   "key",
		"auth":     "secret",
		"active":   true,
	}

	endpoint, err := EndpointFromDocument(Document{ID: "endpoint-1", UserID: "user", Data: valid})
	if err != nil {
		t.Fatalf("endpoint from document: %v", err)
	}
	if endpoint.Key() != "user/endpoint-1" || !endpoint.Active || endpoint.URL != "https://push.test/abc" {
		t.Fatalf("endpoint = %+v; want the projected subscription", endpoint)
	}

	// Firebase omits `active` for subscriptions the client has never confirmed, and
	// the Python query required active == true, so absence must mean inactive.
	withoutActive := map[string]any{"endpoint": "https://push.test/abc", "p256dh": "key", "auth": "secret"}
	if endpoint, err = EndpointFromDocument(Document{ID: "endpoint-1", UserID: "user", Data: withoutActive}); err != nil {
		t.Fatalf("endpoint from document: %v", err)
	}
	if endpoint.Active {
		t.Error("missing active field should project as inactive")
	}

	tests := []struct {
		name string
		doc  Document
	}{
		{name: "no document id", doc: Document{UserID: "user", Data: valid}},
		{name: "no owning user", doc: Document{ID: "endpoint-1", Data: valid}},
		{name: "missing keys", doc: Document{ID: "endpoint-1", UserID: "user", Data: map[string]any{"endpoint": "https://push.test/abc"}}},
		{name: "empty endpoint url", doc: Document{ID: "endpoint-1", UserID: "user", Data: map[string]any{"endpoint": "", "p256dh": "key", "auth": "secret"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := EndpointFromDocument(test.doc); err == nil {
				t.Fatal("EndpointFromDocument succeeded; want error")
			}
		})
	}
}

func TestPrivateUserEqualityChecksEveryRetainedField(t *testing.T) {
	text, flag := "value", false
	baseline := PrivateUserContribution{
		UserID: "user", Email: &text, NotificationEmail: &text, DefaultArrivalTime: &text,
		TestEmailReq: &text, TestEmailAck: &text, WebPushEnabled: &flag, PollOpens: &flag,
		PollCompletes: &flag, GlobalChat: &flag, EventChat: &flag,
		OpenPollEmailEnabled: &flag, NotificationEmailEnabled: &flag,
		EventChatMutedPollIDs: []string{"poll-1"},
	}
	for _, test := range []struct {
		name   string
		change func(*PrivateUserContribution)
	}{
		{"UserID", func(user *PrivateUserContribution) { user.UserID = "other" }},
		{"Email", func(user *PrivateUserContribution) { user.Email = nil }},
		{"NotificationEmail", func(user *PrivateUserContribution) { user.NotificationEmail = nil }},
		{"DefaultArrivalTime", func(user *PrivateUserContribution) { user.DefaultArrivalTime = nil }},
		{"TestEmailReq", func(user *PrivateUserContribution) { user.TestEmailReq = nil }},
		{"TestEmailAck", func(user *PrivateUserContribution) { user.TestEmailAck = nil }},
		{"WebPushEnabled", func(user *PrivateUserContribution) { user.WebPushEnabled = nil }},
		{"PollOpens", func(user *PrivateUserContribution) { user.PollOpens = nil }},
		{"PollCompletes", func(user *PrivateUserContribution) { user.PollCompletes = nil }},
		{"GlobalChat", func(user *PrivateUserContribution) { user.GlobalChat = nil }},
		{"EventChat", func(user *PrivateUserContribution) { user.EventChat = nil }},
		{"OpenPollEmailEnabled", func(user *PrivateUserContribution) { user.OpenPollEmailEnabled = nil }},
		{"NotificationEmailEnabled", func(user *PrivateUserContribution) { user.NotificationEmailEnabled = nil }},
		{"EventChatMutedPollIDs", func(user *PrivateUserContribution) { user.EventChatMutedPollIDs = []string{"other"} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := baseline
			test.change(&changed)
			if baseline.Equal(changed) || changed.Equal(baseline) {
				t.Fatalf("changed %s compared equal", test.name)
			}
		})
	}
	other := baseline
	other.EventChatMutedPollIDs = []string{"poll-1", "poll-1"}
	if !baseline.Equal(other) {
		t.Fatal("duplicate muted IDs changed contribution equality")
	}
	absent := PrivateUserContribution{UserID: "user"}
	empty := PrivateUserContribution{UserID: "user", EventChatMutedPollIDs: []string{}}
	if !absent.Equal(empty) {
		t.Fatal("missing muted IDs differ from an empty set")
	}
}

func TestPrivateUserDefaultsDoNotEraseSourcePresence(t *testing.T) {
	absent, err := PrivateUserFromDocument(Document{ID: "user"})
	if err != nil {
		t.Fatal(err)
	}
	explicit, err := PrivateUserFromDocument(Document{ID: "user", Data: map[string]any{
		"webPushEnabled": false, "notificationEmail": "", "notificationEmailEnabled": false,
		"openPollEmailEnabled": false,
		"pushPreferences":      map[string]any{"pollOpens": true, "pollCompletes": true, "globalChat": false, "eventChat": false},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if absent.Equal(explicit) {
		t.Fatal("source presence was erased by notification defaults")
	}
	if !absent.Preferences().Equal(explicit.Preferences()) {
		t.Fatal("equivalent effective notification defaults differ")
	}
	for _, invalid := range []Document{
		{ID: "user", Data: map[string]any{"notificationEmailEnabled": "yes"}},
		{ID: "user", Data: map[string]any{"pushPreferences": map[string]any{"eventChatMutedPollIds": []any{"valid", 7}}}},
	} {
		if _, err := PreferencesFromDocument(invalid); err == nil {
			t.Fatal("legacy adapter defaulted an invalid supplied value")
		}
	}
}

func TestPublicUserPreservesMissingNullAndZeroValues(t *testing.T) {
	missing, err := PublicUserFromDocument(Document{ID: "user", Data: map[string]any{"uid": "ignored", "email": "ignored"}})
	if err != nil || !missing.Equal(PublicUserContribution{UserID: "user"}) {
		t.Fatalf("public missing fields = %+v, %v", missing, err)
	}
	nulls, err := PublicUserFromDocument(Document{ID: "user", Data: map[string]any{
		"name": nil, "photoUrl": nil, "votesVisible": nil,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !nulls.Name.Present || nulls.Name.Value != nil || !nulls.PhotoURL.Present || nulls.PhotoURL.Value != nil ||
		!nulls.VotesVisible.Present || nulls.VotesVisible.Value != nil || nulls.CustomPhotoURL != nil {
		t.Fatalf("public nulls were not retained: %+v", nulls)
	}
	zeroDoc := Document{ID: "user", Data: map[string]any{"name": "", "photoUrl": "", "customPhotoUrl": "", "votesVisible": false}}
	zero, err := PublicUserFromDocument(zeroDoc)
	if err != nil {
		t.Fatal(err)
	}
	empty, disabled := "", false
	want := PublicUserContribution{
		UserID: "user", Name: NullableField[string]{Present: true, Value: &empty},
		PhotoURL: NullableField[string]{Present: true, Value: &empty}, CustomPhotoURL: &empty,
		VotesVisible: NullableField[bool]{Present: true, Value: &disabled},
	}
	if !zero.Equal(want) || !want.Equal(zero) {
		t.Fatalf("explicit public zero values = %+v; want %+v", zero, want)
	}
	for _, left := range []PublicUserContribution{missing, nulls, zero} {
		if !left.Equal(left) {
			t.Fatal("public contribution is not equal to itself")
		}
	}
	if missing.Equal(nulls) || nulls.Equal(missing) || missing.Equal(zero) || zero.Equal(missing) || nulls.Equal(zero) || zero.Equal(nulls) {
		t.Fatal("missing, null and explicit zero states compared equal")
	}
	for _, data := range []map[string]any{
		{"name": false}, {"photoUrl": 1}, {"votesVisible": "yes"}, {"customPhotoUrl": nil},
	} {
		if _, err := PublicUserFromDocument(Document{ID: "user", Data: data}); err == nil {
			t.Fatalf("accepted invalid public field: %#v", data)
		}
	}
	if _, err := PublicUserFromDocument(Document{}); err == nil {
		t.Fatal("accepted missing public user document ID")
	}
}

func TestPublicUserEqualityChecksEveryRetainedField(t *testing.T) {
	text, flag := "value", false
	baseline := PublicUserContribution{
		UserID: "user", Name: NullableField[string]{Present: true, Value: &text},
		PhotoURL: NullableField[string]{Present: true, Value: &text}, CustomPhotoURL: &text,
		VotesVisible: NullableField[bool]{Present: true, Value: &flag},
	}
	for _, test := range []struct {
		name   string
		change func(*PublicUserContribution)
	}{
		{"UserID", func(user *PublicUserContribution) { user.UserID = "other" }},
		{"Name", func(user *PublicUserContribution) { user.Name.Value = nil }},
		{"PhotoURL", func(user *PublicUserContribution) { user.PhotoURL.Value = nil }},
		{"CustomPhotoURL", func(user *PublicUserContribution) { user.CustomPhotoURL = nil }},
		{"VotesVisible", func(user *PublicUserContribution) { user.VotesVisible.Value = nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := baseline
			test.change(&changed)
			if baseline.Equal(changed) || changed.Equal(baseline) {
				t.Fatalf("changed %s compared equal", test.name)
			}
		})
	}
}

func TestEndpointContributionPreservesPresenceAndExcludesMetadata(t *testing.T) {
	doc := Document{ID: "endpoint", UserID: "user", Data: map[string]any{
		"endpoint": "https://push.test/endpoint", "p256dh": "key", "auth": "auth",
		"uid": "ignored", "userAgent": "browser", "platform": "device", "appVersion": "one",
		"createdAt": time.Now(), "lastSeenAt": time.Now(), "disabledAt": nil,
	}}
	absent, err := EndpointContributionFromDocument(doc)
	if err != nil {
		t.Fatal(err)
	}
	if absent.Active != nil || absent.Endpoint().Active {
		t.Fatal("missing active field should remain absent and effectively inactive")
	}
	doc.Data["userAgent"], doc.Data["platform"], doc.Data["appVersion"] = "other", nil, "two"
	doc.Data["createdAt"], doc.Data["lastSeenAt"], doc.Data["disabledAt"] = time.Now().Add(time.Hour), time.Now().Add(time.Hour), time.Now()
	metadataOnly, err := EndpointContributionFromDocument(doc)
	if err != nil || !absent.Equal(metadataOnly) {
		t.Fatalf("metadata changed versioned endpoint values: %+v, %v", metadataOnly, err)
	}
	doc.Data["active"] = false
	explicit, err := EndpointContributionFromDocument(doc)
	if err != nil {
		t.Fatal(err)
	}
	if absent.Equal(explicit) || explicit.Equal(absent) || !absent.Endpoint().Equal(explicit.Endpoint()) {
		t.Fatal("active presence must change the contribution but not effective inactive behaviour")
	}
	flag := false
	want := EndpointContribution{EndpointID: "endpoint", UserID: "user", URL: "https://push.test/endpoint", P256DH: "key", Auth: "auth", Active: &flag}
	if !explicit.Equal(want) {
		t.Fatal("independent equal active pointers compared unequal")
	}
	for _, invalid := range []any{nil, "yes", 1} {
		doc.Data["active"] = invalid
		if _, err := EndpointFromDocument(doc); err == nil {
			t.Fatalf("legacy endpoint adapter accepted active=%v", invalid)
		}
	}
}

func TestEndpointEqualityChecksEveryRetainedField(t *testing.T) {
	flag := false
	baseline := EndpointContribution{EndpointID: "endpoint", UserID: "user", URL: "url", P256DH: "key", Auth: "auth", Active: &flag}
	for _, test := range []struct {
		name   string
		change func(*EndpointContribution)
	}{
		{"EndpointID", func(endpoint *EndpointContribution) { endpoint.EndpointID = "other" }},
		{"UserID", func(endpoint *EndpointContribution) { endpoint.UserID = "other" }},
		{"URL", func(endpoint *EndpointContribution) { endpoint.URL = "other" }},
		{"P256DH", func(endpoint *EndpointContribution) { endpoint.P256DH = "other" }},
		{"Auth", func(endpoint *EndpointContribution) { endpoint.Auth = "other" }},
		{"Active", func(endpoint *EndpointContribution) { value := true; endpoint.Active = &value }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := baseline
			test.change(&changed)
			if baseline.Equal(changed) || changed.Equal(baseline) || baseline.Endpoint().Equal(changed.Endpoint()) {
				t.Fatalf("changed %s compared equal", test.name)
			}
		})
	}
}

func TestEffectiveEqualityIgnoresIngestionTimestamps(t *testing.T) {
	preferences := UserPreferences{UserID: "user", EventChatMutedPollIDs: []string{"poll-2", "poll-1", "poll-2"}}
	replay := preferences
	replay.UpdatedAt = time.Now()
	replay.EventChatMutedPollIDs = []string{"poll-1", "poll-2"}
	if !preferences.Equal(replay) {
		t.Fatal("timestamp or set ordering changed effective preference equality")
	}
	endpoint := Endpoint{EndpointID: "endpoint", UserID: "user", URL: "url", P256DH: "key", Auth: "auth"}
	other := endpoint
	other.UpdatedAt = time.Now()
	if !endpoint.Equal(other) {
		t.Fatal("ingestion timestamp changed endpoint equality")
	}
}
