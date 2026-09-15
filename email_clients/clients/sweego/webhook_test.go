package sweego

import "testing"

func TestVerifySweegoSignature(t *testing.T) {
	const (
		secret    = "c2VjcmV0LWtleQ=="
		webhookID = "wh_123"
		timestamp = "1725192000"
		signature = "aSPpPN3ktciYuo9gshDbKqUb7RxCbg92DKo77abGiuw="
	)
	body := []byte(`{"event":"delivered"}`)

	if !verifySweegoSignature(secret, webhookID, timestamp, body, signature) {
		t.Fatal("verifySweegoSignature() rejected a valid signature")
	}

	tests := []struct {
		name      string
		secret    string
		webhookID string
		timestamp string
		body      []byte
		signature string
	}{
		{name: "wrong signature", secret: secret, webhookID: webhookID, timestamp: timestamp, body: body, signature: "invalid"},
		{name: "altered body", secret: secret, webhookID: webhookID, timestamp: timestamp, body: []byte(`{"event":"failed"}`), signature: signature},
		{name: "altered timestamp", secret: secret, webhookID: webhookID, timestamp: "1725192001", body: body, signature: signature},
		{name: "altered webhook ID", secret: secret, webhookID: "wh_456", timestamp: timestamp, body: body, signature: signature},
		{name: "invalid base64 secret", secret: "%%%", webhookID: webhookID, timestamp: timestamp, body: body, signature: signature},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if verifySweegoSignature(test.secret, test.webhookID, test.timestamp, test.body, test.signature) {
				t.Fatal("verifySweegoSignature() accepted an invalid signature")
			}
		})
	}
}
