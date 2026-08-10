package main

import (
	"database/sql"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cellar/internal/sqlite"
	"cellar/pkg/cellar"
)

func TestRunInspectFromSQLite(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cells.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}

	store, err := sqlite.NewStore(db, cellar.NewSequentialAllocator("test-", 1))
	if err != nil {
		_ = db.Close()
		t.Fatalf("NewStore() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	payload, err := json.Marshal(map[string]any{"kind": "welcome"})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	_, err = store.Add([]cellar.CellRequest{{
		HandlerName: "send-email",
		Payload:     payload,
	}})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	output, err := captureStdout(func() error {
		return runInspect([]string{
			"-sqlite", dbPath,
			"-json-handlers", "send-email",
		})
	})
	if err != nil {
		t.Fatalf("runInspect() error = %v", err)
	}

	var inspections []map[string]any
	if err := json.Unmarshal([]byte(output), &inspections); err != nil {
		t.Fatalf("inspect output JSON parse error = %v\noutput: %s", err, output)
	}
	if len(inspections) != 1 {
		t.Fatalf("inspect count = %d, want 1", len(inspections))
	}
	if inspections[0]["payloadFormat"] != "json" {
		t.Fatalf("payloadFormat = %v, want json", inspections[0]["payloadFormat"])
	}
}

func TestRunInspectFromSQLiteFixtureExactShape(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cells.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}

	store, err := sqlite.NewStore(db, cellar.NewSequentialAllocator("test-", 1))
	if err != nil {
		_ = db.Close()
		t.Fatalf("NewStore() error = %v", err)
	}

	notBefore := time.Date(2026, 8, 8, 9, 30, 0, 0, time.UTC)
	jsonPayload, err := json.Marshal(map[string]any{"kind": "welcome", "attempt": float64(1)})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	_, err = store.Add([]cellar.CellRequest{
		{
			HandlerName: "json-handler",
			Payload:     jsonPayload,
		},
		{
			HandlerName: "raw-handler",
			Payload:     []byte{0xff, 0x00, 0x41},
			NotBefore:   &notBefore,
		},
	})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	_, ok, err := store.ClaimNext(time.Now())
	if err != nil {
		t.Fatalf("ClaimNext() error = %v", err)
	}
	if !ok {
		t.Fatalf("ClaimNext() ok = false, want true")
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	output, err := captureStdout(func() error {
		return runInspect([]string{
			"-sqlite", dbPath,
			"-json-handlers", "json-handler",
		})
	})
	if err != nil {
		t.Fatalf("runInspect() error = %v", err)
	}

	const expected = `[
  {
    "id": "test-1",
    "handlerName": "json-handler",
    "state": "CLAIMED",
    "payloadFormat": "json",
    "payload": {
      "attempt": 1,
      "kind": "welcome"
    }
  },
  {
    "id": "test-2",
    "handlerName": "raw-handler",
    "state": "READY",
    "notBefore": "2026-08-08T09:30:00Z",
    "payloadFormat": "base64",
    "payload": "/wBB"
  }
]
`

	if output != expected {
		t.Fatalf("inspect output mismatch\nexpected:\n%s\nactual:\n%s", expected, output)
	}
}

func TestRunInspectFromSQLiteByIDFixtureExactShape(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cells.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}

	store, err := sqlite.NewStore(db, cellar.NewSequentialAllocator("test-", 1))
	if err != nil {
		_ = db.Close()
		t.Fatalf("NewStore() error = %v", err)
	}

	payload, err := json.Marshal(map[string]any{"kind": "target", "priority": float64(3)})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	_, err = store.Add([]cellar.CellRequest{
		{HandlerName: "json-handler", Payload: payload},
		{HandlerName: "other-handler", Payload: []byte("ignore-me")},
	})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	output, err := captureStdout(func() error {
		return runInspect([]string{
			"-sqlite", dbPath,
			"-id", "test-1",
			"-json-handlers", "json-handler",
		})
	})
	if err != nil {
		t.Fatalf("runInspect() error = %v", err)
	}

	const expected = `{
  "id": "test-1",
  "handlerName": "json-handler",
  "state": "READY",
  "payloadFormat": "json",
  "payload": {
    "kind": "target",
    "priority": 3
  }
}
`

	if output != expected {
		t.Fatalf("inspect by ID output mismatch\nexpected:\n%s\nactual:\n%s", expected, output)
	}
}

func captureStdout(run func() error) (string, error) {
	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		return "", err
	}
	os.Stdout = writer

	runErr := run()
	_ = writer.Close()
	os.Stdout = original

	bytes, readErr := io.ReadAll(reader)
	if readErr != nil {
		return "", readErr
	}
	if runErr != nil {
		return "", runErr
	}
	return string(bytes), nil
}
