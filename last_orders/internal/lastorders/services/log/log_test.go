package log

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/truths"
)

func TestHandlerLogsMessageAndCompletes(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))

	result := Handler{Logger: logger}.Handle(context.Background(), truths.LogMessage{Message: "hello world"})

	if _, ok := result.(cellar.Complete); !ok {
		t.Fatalf("expected cellar.Complete, got %T", result)
	}
	if !strings.Contains(output.String(), "hello world") {
		t.Fatalf("expected message to be logged, got: %s", output.String())
	}
}

func TestHandlerToleratesMissingLogger(t *testing.T) {
	result := Handler{}.Handle(context.Background(), truths.LogMessage{Message: "hello world"})

	if _, ok := result.(cellar.Complete); !ok {
		t.Fatalf("expected cellar.Complete, got %T", result)
	}
}

func TestPayloadRoundTripsThroughJSONCodec(t *testing.T) {
	codec := cellar.JSONCodec[truths.LogMessage]()

	raw, err := codec.Marshal(truths.LogMessage{Message: "hello world"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded truths.LogMessage
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Message != "hello world" {
		t.Fatalf("expected message %q, got %q", "hello world", decoded.Message)
	}
}
