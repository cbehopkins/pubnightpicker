package handlers

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"cellar/pkg/cellar"
)

func TestPollHandlersLogProcessedObservation(t *testing.T) {
	tests := []struct {
		name    string
		handler cellar.Handler[PollObservedPayload]
		payload PollObservedPayload
		message string
	}{
		{
			name:    "new poll",
			payload: PollObservedPayload{PollID: "poll-1"},
			message: "new poll processed",
		},
		{
			name:    "completed poll",
			payload: PollObservedPayload{PollID: "poll-2", ChangeKind: "modified"},
			message: "completed poll processed",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&output, nil))
			if test.name == "new poll" {
				test.handler = NewPollHandler{Logger: logger}
			} else {
				test.handler = CompletedPollHandler{Logger: logger}
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
