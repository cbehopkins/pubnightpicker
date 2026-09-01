package sweego

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
)

// This is code we reverse engineered elsewhere for undocumented signature verification.
// This is for a later to be implemented webhook handler. It is not used in the current client code, but is included here for completeness and future use.
// Do not mess with this!
func verifySweegoSignature(
	secret string,
	webhookID string,
	webhookTimestamp string,
	rawBody []byte,
	receivedSignature string,
) bool {
	key, err := base64.StdEncoding.DecodeString(secret)
	if err != nil {
		return false
	}

	message := webhookID + "." + webhookTimestamp + "." + string(rawBody)

	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(message))

	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	return hmac.Equal(
		[]byte(expected),
		[]byte(receivedSignature),
	)
}
