package venues

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"golang.org/x/crypto/hkdf"
)

const (
	deleteTokenDomain  = "pubnightpicker:venue-document-delete:v1\x00"
	deleteTokenPrefix  = "v1"
	maximumDeleteToken = 4096
)

var ErrInvalidDeleteToken = errors.New("invalid venue delete token")

type DeleteTokenCodec struct {
	key []byte
}

type deleteTokenClaims struct {
	VenueID    string `json:"venue_id"`
	UpdateTime string `json:"update_time"`
}

func NewDeleteTokenCodec(cursorKey []byte) (*DeleteTokenCodec, error) {
	if len(cursorKey) < 32 {
		return nil, errors.New("venue delete token source key must contain at least 32 bytes")
	}
	reader := hkdf.New(sha256.New, cursorKey, nil, []byte("pubnightpicker:venue-document-delete:v1"))
	key := make([]byte, sha256.Size)
	if _, err := io.ReadFull(reader, key); err != nil {
		return nil, err
	}
	return &DeleteTokenCodec{key: key}, nil
}

func (c *DeleteTokenCodec) Issue(venueID string, updateTime time.Time) (string, error) {
	if c == nil || len(c.key) != sha256.Size || venueID == "" {
		return "", ErrInvalidDeleteToken
	}
	payload, err := json.Marshal(deleteTokenClaims{
		VenueID: venueID, UpdateTime: updateTime.UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, c.key)
	_, _ = io.WriteString(mac, deleteTokenDomain+deleteTokenPrefix+"."+encoded)
	token := deleteTokenPrefix + "." + encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if len(token) > maximumDeleteToken {
		return "", ErrInvalidDeleteToken
	}
	return token, nil
}

func (c *DeleteTokenCodec) Validate(token, venueID string) (time.Time, error) {
	if c == nil || len(c.key) != sha256.Size || token == "" || len(token) > maximumDeleteToken {
		return time.Time{}, ErrInvalidDeleteToken
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != deleteTokenPrefix {
		return time.Time{}, ErrInvalidDeleteToken
	}
	payload, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if err != nil || len(payload) == 0 {
		return time.Time{}, ErrInvalidDeleteToken
	}
	providedMAC, err := base64.RawURLEncoding.Strict().DecodeString(parts[2])
	if err != nil || len(providedMAC) != sha256.Size {
		return time.Time{}, ErrInvalidDeleteToken
	}
	mac := hmac.New(sha256.New, c.key)
	_, _ = io.WriteString(mac, deleteTokenDomain+deleteTokenPrefix+"."+parts[1])
	if !hmac.Equal(providedMAC, mac.Sum(nil)) {
		return time.Time{}, ErrInvalidDeleteToken
	}
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	var claims deleteTokenClaims
	if err := decoder.Decode(&claims); err != nil || claims.VenueID != venueID || claims.UpdateTime == "" {
		return time.Time{}, ErrInvalidDeleteToken
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return time.Time{}, ErrInvalidDeleteToken
	}
	updateTime, err := time.Parse(time.RFC3339Nano, claims.UpdateTime)
	if err != nil || claims.UpdateTime != updateTime.UTC().Format(time.RFC3339Nano) {
		return time.Time{}, ErrInvalidDeleteToken
	}
	return updateTime, nil
}
