package cli

import (
	"testing"
)

func TestParseBulkResponseSupportsObservedAndPerRecipientShapes(t *testing.T) {
	for _, body := range []string{
		`{"transaction_id":"T1","swg_uids":{"alice@example.com":"U1","bob@example.com":"U2"}}`,
		`{"transaction_id":"T1","messages":[{"recipient":"alice@example.com","swg_uid":"U1"},{"email":"bob@example.com","swg_uid":"U2"}]}`,
	} {
		response, err := parseBulkResponse([]byte(body))
		if err != nil {
			t.Fatal(err)
		}
		if response.TransactionID != "T1" || response.SwgUIDs["alice@example.com"] != "U1" || response.SwgUIDs["bob@example.com"] != "U2" {
			t.Fatalf("unexpected parsed response: %+v", response)
		}
	}
}
