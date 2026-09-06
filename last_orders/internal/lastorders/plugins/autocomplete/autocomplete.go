package autocomplete

import (
	"last_orders/internal/lastorders/components/facts"
	"last_orders/internal/lastorders/services/autocomplete"
	"last_orders/internal/lastorders/truths"
)

func Register(registry *facts.Registry) {
	registry.Register(truths.DailyPollAutoCompleteDueName, autocomplete.HandlerDiscovery)
	registry.Register(truths.PollAutoCompletionDueName, autocomplete.HandlerCandidate)
}
