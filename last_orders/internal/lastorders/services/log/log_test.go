package log

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"cellar/pkg/cellar"
)

func TestHandlerLogsMessageAndCompletes(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))

	result := Handler{Logger: logger}.Handle(context.Background(), Payload{Message: "hello world"})

	if _, ok := result.(cellar.Complete); !ok {
		t.Fatalf("expected cellar.Complete, got %T", result)
	}
	if !strings.Contains(output.String(), "hello world") {
		t.Fatalf("expected message to be logged, got: %s", output.String())
	}
}

func TestHandlerToleratesMissingLogger(t *testing.T) {
	result := Handler{}.Handle(context.Background(), Payload{Message: "hello world"})

	if _, ok := result.(cellar.Complete); !ok {
		t.Fatalf("expected cellar.Complete, got %T", result)
	}
}

func TestPayloadRoundTripsThroughJSONCodec(t *testing.T) {
	codec := cellar.JSONCodec[Payload]()

	raw, err := codec.Marshal(Payload{Message: "hello world"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Payload
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Message != "hello world" {
		t.Fatalf("expected message %q, got %q", "hello world", decoded.Message)
	}
}
