package sse

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	cursorPrefix       = "v1"
	cursorDomain       = "pubnightpicker:sse-cursor:v1\x00"
	minimumCursorKey   = 32
	maximumCursorToken = 4096
)

var ErrInvalidCursor = errors.New("invalid SSE cursor")

// CursorClaims bind a cursor to one stream and one consistent read-model
// position. Date is set only for date-scoped streams such as Current-Meets.
type CursorClaims struct {
	Stream     string `json:"stream"`
	Resource   string `json:"resource"`
	Generation string `json:"generation"`
	Sequence   int64  `json:"sequence"`
	Date       string `json:"date,omitempty"`
}

// CursorExpectation describes the scope and upper sequence bound for a cursor.
type CursorExpectation struct {
	Stream     string
	Resource   string
	Generation string
	HighWater  int64
	Date       string
}

// CursorCodec creates and verifies opaque, integrity-protected cursors.
type CursorCodec struct {
	key []byte
}

func NewCursorCodec(key []byte) (*CursorCodec, error) {
	if len(key) < minimumCursorKey {
		return nil, fmt.Errorf("cursor HMAC key must contain at least %d bytes", minimumCursorKey)
	}
	return &CursorCodec{key: append([]byte(nil), key...)}, nil
}

// ParseCursorKey decodes the runtime secret's unpadded base64url form.
func ParseCursorKey(encoded string) ([]byte, error) {
	key, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || len(key) < minimumCursorKey {
		return nil, fmt.Errorf("cursor HMAC key must be base64url for at least %d bytes", minimumCursorKey)
	}
	return key, nil
}

func (c *CursorCodec) Encode(claims CursorClaims) (string, error) {
	if c == nil || len(c.key) < minimumCursorKey || !validClaims(claims) {
		return "", ErrInvalidCursor
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", ErrInvalidCursor
	}
	if len(payload) > 2048 {
		return "", ErrInvalidCursor
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, c.key)
	_, _ = io.WriteString(mac, cursorDomain+cursorPrefix+"."+encodedPayload)
	encodedMAC := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	token := cursorPrefix + "." + encodedPayload + "." + encodedMAC
	if len(token) > maximumCursorToken {
		return "", ErrInvalidCursor
	}
	return token, nil
}

func (c *CursorCodec) Decode(token string) (CursorClaims, error) {
	if c == nil || len(c.key) < minimumCursorKey || token == "" || len(token) > maximumCursorToken {
		return CursorClaims{}, ErrInvalidCursor
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != cursorPrefix {
		return CursorClaims{}, ErrInvalidCursor
	}
	payload, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if err != nil || len(payload) == 0 || len(payload) > 2048 {
		return CursorClaims{}, ErrInvalidCursor
	}
	providedMAC, err := base64.RawURLEncoding.Strict().DecodeString(parts[2])
	if err != nil || len(providedMAC) != sha256.Size {
		return CursorClaims{}, ErrInvalidCursor
	}
	mac := hmac.New(sha256.New, c.key)
	_, _ = io.WriteString(mac, cursorDomain+cursorPrefix+"."+parts[1])
	if !hmac.Equal(providedMAC, mac.Sum(nil)) {
		return CursorClaims{}, ErrInvalidCursor
	}

	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	var claims CursorClaims
	if err := decoder.Decode(&claims); err != nil || !validClaims(claims) {
		return CursorClaims{}, ErrInvalidCursor
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return CursorClaims{}, ErrInvalidCursor
	}
	return claims, nil
}

func (c *CursorCodec) Validate(token string, expected CursorExpectation) (CursorClaims, error) {
	claims, err := c.Decode(token)
	if err != nil || claims.Stream != expected.Stream || claims.Resource != expected.Resource || claims.Generation != expected.Generation || claims.Sequence > expected.HighWater || claims.Date != expected.Date || expected.HighWater < 0 {
		return CursorClaims{}, ErrInvalidCursor
	}
	return claims, nil
}

// RequestCursor selects Last-Event-ID before the optional after query value.
// The bool distinguishes an absent cursor from a supplied empty/invalid value.
func RequestCursor(r *http.Request) (value string, supplied bool) {
	if r == nil {
		return "", false
	}
	if values, ok := r.Header[http.CanonicalHeaderKey("Last-Event-ID")]; ok {
		if len(values) != 1 {
			return "", true
		}
		return values[0], true
	}
	if r.URL == nil {
		return "", false
	}
	values, ok := r.URL.Query()["after"]
	if !ok {
		return "", false
	}
	if len(values) != 1 {
		return "", true
	}
	return values[0], true
}

func validClaims(claims CursorClaims) bool {
	if claims.Stream == "" || claims.Resource == "" || claims.Generation == "" || claims.Sequence < 0 || strings.ContainsAny(claims.Stream+claims.Resource+claims.Generation+claims.Date, "\x00\r\n") {
		return false
	}
	if claims.Date != "" {
		parsed, err := time.Parse("2006-01-02", claims.Date)
		if err != nil || parsed.Format("2006-01-02") != claims.Date {
			return false
		}
	}
	return true
}
