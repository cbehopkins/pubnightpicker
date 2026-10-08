package clients

import (
	"reflect"
	"testing"
)

func TestPrepareDryRun(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		for _, hosted := range []bool{false, true} {
			email := Email{
				Subject: "Hello", Headers: map[string]string{CorrelationHeader: "pn-1", "x-dry-run": "false"},
				Variables: map[string]any{"name": "Guest", NotificationPrefixVariable: "wrong"},
				To:        []Recipient{{Variables: map[string]any{"name": "Alice", NotificationPrefixVariable: "override"}}},
			}
			if hosted {
				email.TemplateID, email.Subject = "template", ""
			}
			prepared := PrepareDryRun(email, dryRun)
			if prepared.Headers[CorrelationHeader] != "pn-1" {
				t.Fatal("correlation header lost")
			}
			if dryRun && (prepared.Headers[DryRunHeader] != "true" || prepared.Headers["x-dry-run"] != "") {
				t.Fatalf("dry-run headers = %v", prepared.Headers)
			}
			wantSubject := "Hello"
			if dryRun {
				wantSubject = DryRunSubjectPrefix + wantSubject
			}
			if hosted {
				wantSubject = ""
				wantPrefix := ""
				if dryRun {
					wantPrefix = DryRunSubjectPrefix
				}
				if prepared.Variables[NotificationPrefixVariable] != wantPrefix {
					t.Fatalf("prefix = %v", prepared.Variables)
				}
				if _, exists := prepared.To[0].Variables[NotificationPrefixVariable]; exists {
					t.Fatal("recipient can override prefix")
				}
				if prepared.To[0].Variables["name"] != "Alice" {
					t.Fatal("recipient personalisation lost")
				}
			}
			if prepared.Subject != wantSubject {
				t.Fatalf("subject = %q, want %q", prepared.Subject, wantSubject)
			}
			if email.Headers["x-dry-run"] != "false" || email.Headers[DryRunHeader] != "" || email.Variables[NotificationPrefixVariable] != "wrong" || email.To[0].Variables[NotificationPrefixVariable] != "override" {
				t.Fatal("caller input mutated")
			}
			if again := PrepareDryRun(prepared, dryRun); !reflect.DeepEqual(again, prepared) {
				t.Fatal("preparation is not idempotent")
			}
		}
	}
}

func TestPrepareDryRunPreservesRawEndpointSelection(t *testing.T) {
	email := Email{Subject: "Hello", To: []Recipient{{}}}
	for _, dryRun := range []bool{false, true} {
		prepared := PrepareDryRun(email, dryRun)
		if len(prepared.Variables) != 0 || len(prepared.To[0].Variables) != 0 {
			t.Fatal("raw send gained template variables")
		}
		if !dryRun && !reflect.DeepEqual(prepared, email) {
			t.Fatal("normal raw email changed")
		}
	}
}
