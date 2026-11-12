package sse

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCursorCodecRoundTripAndValidation(t *testing.T) {
	codec := testCodec(t, 7)
	claims := CursorClaims{
		Stream: "current-meets-v1", Resource: "group-42", Generation: "generation-1",
		Sequence: 12, Date: "2026-10-26",
	}
	token, err := codec.Encode(claims)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, "v1.") {
		t.Fatalf("token prefix=%q", token)
	}
	if got, err := codec.Decode(token); err != nil || got != claims {
		t.Fatalf("decoded claims=%+v err=%v", got, err)
	}
	got, err := codec.Validate(token, CursorExpectation{
		Stream: claims.Stream, Resource: claims.Resource, Generation: claims.Generation,
		HighWater: 12, Date: claims.Date,
	})
	if err != nil || got != claims {
		t.Fatalf("validated claims=%+v err=%v", got, err)
	}
}

func TestCursorRejectsTamperingAndKeyRotation(t *testing.T) {
	codec := testCodec(t, 1)
	claims := CursorClaims{Stream: "attendance-v1", Resource: "meet-1", Generation: "gen", Sequence: 5}
	token, err := codec.Encode(claims)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	parts[1] = "A" + parts[1][1:]
	if _, err := codec.Decode(strings.Join(parts, ".")); err == nil {
		t.Fatal("tampered cursor was accepted")
	}
	if _, err := testCodec(t, 2).Decode(token); err == nil {
		t.Fatal("cursor signed by a rotated key was accepted")
	}
	if _, err := codec.Decode("v2." + strings.Join(parts[1:], ".")); err == nil {
		t.Fatal("unsupported cursor version was accepted")
	}
}

func TestCursorValidationRejectsScopeGenerationDateAndSequenceMismatch(t *testing.T) {
	codec := testCodec(t, 3)
	token, err := codec.Encode(CursorClaims{
		Stream: "current-meets-v1", Resource: "group-a", Generation: "gen-a", Sequence: 12, Date: "2026-10-26",
	})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		want CursorExpectation
	}{
		{name: "wrong stream", want: CursorExpectation{Stream: "attendance-v1", Resource: "group-a", Generation: "gen-a", HighWater: 20, Date: "2026-10-26"}},
		{name: "wrong resource", want: CursorExpectation{Stream: "current-meets-v1", Resource: "group-b", Generation: "gen-a", HighWater: 20, Date: "2026-10-26"}},
		{name: "wrong generation", want: CursorExpectation{Stream: "current-meets-v1", Resource: "group-a", Generation: "gen-b", HighWater: 20, Date: "2026-10-26"}},
		{name: "wrong date", want: CursorExpectation{Stream: "current-meets-v1", Resource: "group-a", Generation: "gen-a", HighWater: 20, Date: "2026-10-27"}},
		{name: "future sequence", want: CursorExpectation{Stream: "current-meets-v1", Resource: "group-a", Generation: "gen-a", HighWater: 11, Date: "2026-10-26"}},
		{name: "negative high water", want: CursorExpectation{Stream: "current-meets-v1", Resource: "group-a", Generation: "gen-a", HighWater: -1, Date: "2026-10-26"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := codec.Validate(token, test.want); err == nil {
				t.Fatal("mismatched cursor accepted")
			}
		})
	}
}

func TestCursorRejectsMalformedPayloadAndSequences(t *testing.T) {
	codec := testCodec(t, 4)
	for _, claims := range []CursorClaims{
		{Stream: "", Resource: "r", Generation: "g", Sequence: 0},
		{Stream: "s", Resource: "", Generation: "g", Sequence: 0},
		{Stream: "s", Resource: "r", Generation: "", Sequence: 0},
		{Stream: "s", Resource: "r", Generation: "g", Sequence: -1},
		{Stream: "s\n", Resource: "r", Generation: "g", Sequence: 0},
		{Stream: "s", Resource: "r", Generation: "g", Sequence: 0, Date: "2026-02-30"},
	} {
		if _, err := codec.Encode(claims); err == nil {
			t.Fatalf("invalid claims accepted: %+v", claims)
		}
	}
	if _, err := codec.Decode(strings.Repeat("x", maximumCursorToken+1)); err == nil {
		t.Fatal("oversized token accepted")
	}
}

func TestParseCursorKeyRequiresBase64URL32Bytes(t *testing.T) {
	key := make([]byte, minimumCursorKey)
	encoded := base64.RawURLEncoding.EncodeToString(key)
	if got, err := ParseCursorKey(encoded); err != nil || len(got) != minimumCursorKey {
		t.Fatalf("parsed key length=%d err=%v", len(got), err)
	}
	if _, err := ParseCursorKey("too-short"); err == nil {
		t.Fatal("short key accepted")
	}
	if _, err := NewCursorCodec(key[:minimumCursorKey-1]); err == nil {
		t.Fatal("codec accepted short key")
	}
}

func TestRequestCursorPrecedenceAndPresence(t *testing.T) {
	tests := []struct {
		name     string
		target   string
		header   string
		want     string
		supplied bool
	}{
		{name: "absent", target: "/stream", supplied: false},
		{name: "after", target: "/stream?after=query", want: "query", supplied: true},
		{name: "header wins", target: "/stream?after=query", header: "event-id", want: "event-id", supplied: true},
		{name: "empty header wins", target: "/stream?after=query", header: "", want: "", supplied: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", test.target, nil)
			if test.name == "empty header wins" {
				r.Header[http.CanonicalHeaderKey("Last-Event-ID")] = []string{""}
			} else if test.header != "" {
				r.Header.Set("Last-Event-ID", test.header)
			}
			got, supplied := RequestCursor(r)
			if got != test.want || supplied != test.supplied {
				t.Fatalf("RequestCursor()=(%q,%v), want (%q,%v)", got, supplied, test.want, test.supplied)
			}
		})
	}
}

func testCodec(t *testing.T, fill byte) *CursorCodec {
	t.Helper()
	key := make([]byte, minimumCursorKey)
	for i := range key {
		key[i] = fill
	}
	codec, err := NewCursorCodec(key)
	if err != nil {
		t.Fatal(err)
	}
	return codec
}
