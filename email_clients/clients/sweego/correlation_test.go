package sweego

import (
	"regexp"
	"testing"
)

func TestNewCorrelationIDFormatAndUniqueness(t *testing.T) {
	pattern := regexp.MustCompile(`^pn-[0-9a-f]{32}$`)
	seen := make(map[string]struct{}, 100)
	for range 100 {
		id, err := NewCorrelationID()
		if err != nil {
			t.Fatal(err)
		}
		if !pattern.MatchString(id) {
			t.Fatalf("NewCorrelationID() returned invalid ID %q", id)
		}
		if _, exists := seen[id]; exists {
			t.Fatalf("NewCorrelationID() returned duplicate ID %q", id)
		}
		seen[id] = struct{}{}
	}
}
