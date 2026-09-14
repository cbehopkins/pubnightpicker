package autocomplete

import (
	"last_orders/internal/lastorders/services/autocomplete"
	"last_orders/internal/lastorders/truths"
)

func Register() {
	truths.DailyPollAutoCompleteDueRegistry.Register(autocomplete.HandlerDiscovery)
	truths.PollAutoCompletionDueRegistry.Register(autocomplete.HandlerCandidate)
}
