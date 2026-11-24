package attendancestats

import (
	"testing"

	"last_orders/internal/lastorders/components/attendancecache"
)

func TestEffectiveCanComeAppliesStandingExceptionsAndRestaurantRule(t *testing.T) {
	entries := map[string]attendancecache.Entry{
		"any":          {CanCome: []string{"alice", "bob"}, CannotCome: []string{}},
		"pub-a":        {CanCome: []string{"carol"}, CannotCome: []string{"bob"}},
		"restaurant-a": {CanCome: []string{"bob"}, CannotCome: []string{}},
	}

	gotPub := effectiveCanCome(entries, "pub-a", "pub")
	wantPub := []string{"alice", "bob", "carol"}
	wantPub = exclude(wantPub, []string{"bob"})
	if len(gotPub) != len(wantPub) || gotPub[0] != "alice" || gotPub[1] != "carol" {
		t.Fatalf("pub effective can-come = %v, want %v", gotPub, wantPub)
	}

	gotRestaurant := effectiveCanCome(entries, "restaurant-a", "restaurant")
	if len(gotRestaurant) != 1 || gotRestaurant[0] != "bob" {
		t.Fatalf("restaurant effective can-come = %v, want [bob]", gotRestaurant)
	}
}
