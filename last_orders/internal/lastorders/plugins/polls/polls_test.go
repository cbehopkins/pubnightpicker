package polls

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/truths"
)

func TestPollHandlersLogProcessedObservation(t *testing.T) {
	tests := []struct {
		name    string
		handler cellar.Handler[truths.PollObservedPayload]
		payload truths.PollObservedPayload
		message string
	}{
		{
			name:    "new poll",
			payload: truths.PollObservedPayload{PollID: "poll-1"},
			message: "poll opened processed",
		},
		{
			name:    "completed poll",
			payload: truths.PollObservedPayload{PollID: "poll-2", ChangeKind: "modified"},
			message: "poll completed processed",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&output, nil))
			if test.name == "new poll" {
				test.handler = PollOpenedHandler{Logger: logger}
			} else {
				test.handler = PollCompletedHandler{Logger: logger}
			}

			if _, ok := test.handler.Handle(context.Background(), test.payload).(cellar.Complete); !ok {
				t.Fatal("handler did not complete")
			}
			logged := output.String()
			if !strings.Contains(logged, test.message) || !strings.Contains(logged, test.payload.PollID) {
				t.Fatalf("unexpected log output: %s", logged)
			}
		})
	}
}
