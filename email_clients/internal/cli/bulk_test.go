package cli

import (
	"testing"
)

func TestParseRecipientsUsesGenericRecipientAddresses(t *testing.T) {
	recipients, err := parseRecipients("Alice <alice@example.com>, bob@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(recipients) != 2 || recipients[0].Email != "alice@example.com" || recipients[0].Name != "Alice" || recipients[1].Email != "bob@example.com" {
		t.Fatalf("recipients = %#v", recipients)
	}
}
