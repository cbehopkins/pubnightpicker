package main

import (
	"io"
	"log/slog"
	"testing"

	emailplugin "last_orders/internal/lastorders/plugins/email"
)

func TestEmailOptionsFromEnvSelectsMailtrap(t *testing.T) {
	options, err := emailOptionsFromEnv(testLogger(), func(name string) string {
		if name == "MAILTRAP_TOKEN" {
			return "mailtrap-token"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("email options: %v", err)
	}
	if options.Client != emailplugin.ClientMailtrap || options.MailtrapToken != "mailtrap-token" {
		t.Fatalf("email options = %+v, want Mailtrap client and token", options)
	}
}

func TestEmailOptionsFromEnvRejectsBothProviderTokens(t *testing.T) {
	_, err := emailOptionsFromEnv(testLogger(), func(name string) string {
		if name == "MAILTRAP_TOKEN" || name == "SWEEGO_TOKEN" {
			return "token"
		}
		return ""
	})
	if err == nil {
		t.Fatal("both provider tokens returned nil error")
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
