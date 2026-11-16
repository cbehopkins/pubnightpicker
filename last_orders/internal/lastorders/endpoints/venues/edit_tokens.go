package venues

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"last_orders/internal/lastorders/components/venuecache"

	"golang.org/x/crypto/hkdf"
)

const (
	editTokenDomain  = "pubnightpicker:venue-field-edit:v1\x00"
	editTokenPrefix  = "v1"
	maximumEditToken = 4096
)

var ErrInvalidEditToken = errors.New("invalid venue edit token")

type EditTokenCodec struct {
	key []byte
}

type editTokenClaims struct {
	VenueID     string `json:"venue_id"`
	Field       string `json:"field"`
	ValueDigest string `json:"value_digest"`
}

func NewEditTokenCodec(cursorKey []byte) (*EditTokenCodec, error) {
	if len(cursorKey) < 32 {
		return nil, errors.New("venue edit token source key must contain at least 32 bytes")
	}
	reader := hkdf.New(sha256.New, cursorKey, nil, []byte("pubnightpicker:venue-field-edit:v1"))
	key := make([]byte, sha256.Size)
	if _, err := io.ReadFull(reader, key); err != nil {
		return nil, err
	}
	return &EditTokenCodec{key: key}, nil
}

func (c *EditTokenCodec) Issue(venueID, field string, value any) (string, error) {
	if c == nil || len(c.key) != sha256.Size || venueID == "" || field == "" {
		return "", ErrInvalidEditToken
	}
	digest, err := venuecache.CanonicalValueDigest(value)
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(editTokenClaims{VenueID: venueID, Field: field, ValueDigest: digest})
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, c.key)
	_, _ = io.WriteString(mac, editTokenDomain+editTokenPrefix+"."+encoded)
	token := editTokenPrefix + "." + encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if len(token) > maximumEditToken {
		return "", ErrInvalidEditToken
	}
	return token, nil
}

func (c *EditTokenCodec) Validate(token, venueID, field string) (string, error) {
	if c == nil || len(c.key) != sha256.Size || token == "" || len(token) > maximumEditToken {
		return "", ErrInvalidEditToken
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != editTokenPrefix {
		return "", ErrInvalidEditToken
	}
	payload, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if err != nil || len(payload) == 0 {
		return "", ErrInvalidEditToken
	}
	providedMAC, err := base64.RawURLEncoding.Strict().DecodeString(parts[2])
	if err != nil || len(providedMAC) != sha256.Size {
		return "", ErrInvalidEditToken
	}
	mac := hmac.New(sha256.New, c.key)
	_, _ = io.WriteString(mac, editTokenDomain+editTokenPrefix+"."+parts[1])
	if !hmac.Equal(providedMAC, mac.Sum(nil)) {
		return "", ErrInvalidEditToken
	}
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	var claims editTokenClaims
	if err := decoder.Decode(&claims); err != nil || claims.VenueID != venueID || claims.Field != field || claims.ValueDigest == "" {
		return "", ErrInvalidEditToken
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return "", ErrInvalidEditToken
	}
	return claims.ValueDigest, nil
}
