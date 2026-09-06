package notificationprofile

import "testing"

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
}

func TestPreferencesFromDocumentReadsNestedPreferences(t *testing.T) {
	preferences, err := PreferencesFromDocument(Document{ID: "user", Data: map[string]any{
		"webPushEnabled": true,
		"pushPreferences": map[string]any{
			"pollOpens":             false,
			"globalChat":            true,
			"eventChatMutedPollIds": []any{"poll-1", "", 7, "poll-2"},
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
	if !equalStrings(preferences.EventChatMutedPollIDs, []string{"poll-1", "poll-2"}) {
		t.Fatalf("muted poll ids = %v; want non-string entries dropped", preferences.EventChatMutedPollIDs)
	}
}

func TestPreferencesEnabledRespectsMasterSwitch(t *testing.T) {
	preferences := UserPreferences{WebPushEnabled: false, PollOpens: true}
	if preferences.Enabled(KindPollOpens) {
		t.Error("master switch off should disable poll opens")
	}
	if !preferences.Enabled(KindDiagnostic) {
		t.Error("diagnostic push should not be gated on preferences")
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
