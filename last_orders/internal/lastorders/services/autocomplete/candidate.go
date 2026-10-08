package autocomplete

import "sort"

const (
	ReasonNoVotes               = "no_votes"
	ReasonTie                   = "tie"
	ReasonWinnerNotFoodEligible = "winner_not_food_eligible"
	ReasonWinnerVenueMissing    = "winner_venue_missing"
)

type Decision struct {
	SelectedVenueID string
	Reason          string
}

func (decision Decision) IsClearWinner() bool {
	return decision.SelectedVenueID != ""
}

// Decide selects a deterministic automatic-completion outcome from current
// candidate evidence. Caller-provided venue eligibility applies only to a
// multi-venue vote winner.
func Decide(venueIDs []string, voteCounts map[string]int, foodEligible map[string]bool, venueExists map[string]bool) Decision {
	if len(venueIDs) == 1 {
		return Decision{SelectedVenueID: venueIDs[0]}
	}
	if len(venueIDs) == 0 {
		return Decision{Reason: ReasonNoVotes}
	}

	ordered := append([]string(nil), venueIDs...)
	sort.Strings(ordered)
	winningVenueID := ""
	highestVotes := 0
	tied := false
	for _, venueID := range ordered {
		count := voteCounts[venueID]
		if count > highestVotes {
			highestVotes = count
			winningVenueID = venueID
			tied = false
		} else if count > 0 && count == highestVotes {
			tied = true
		}
	}
	if highestVotes == 0 {
		return Decision{Reason: ReasonNoVotes}
	}
	if tied {
		return Decision{Reason: ReasonTie}
	}
	if !venueExists[winningVenueID] {
		return Decision{Reason: ReasonWinnerVenueMissing}
	}
	if !foodEligible[winningVenueID] {
		return Decision{Reason: ReasonWinnerNotFoodEligible}
	}
	return Decision{SelectedVenueID: winningVenueID}
}
