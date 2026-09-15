package sweego

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// PubnightMessageIDHeader is the application-owned custom header used to
// correlate an outgoing email with its record in Sweego's logs.
const PubnightMessageIDHeader = "X-Pubnight-Message-ID"

// NewCorrelationID generates a unique, application-owned message ID to embed
// in the PubnightMessageIDHeader before a send attempt.
func NewCorrelationID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate correlation id: %w", err)
	}
	return "pn-" + hex.EncodeToString(buf), nil
}
