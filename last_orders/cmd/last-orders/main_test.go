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

func TestDailyLimitFromEnv(t *testing.T) {
	for _, name := range []string{"LAST_ORDERS_EMAIL_DAILY_LIMIT", "LAST_ORDERS_PUSH_DAILY_LIMIT"} {
		for _, test := range []struct {
			raw        string
			configured bool
			want       int
		}{
			{want: 100},
			{raw: "250", configured: true, want: 250},
			{raw: " 250 ", configured: true, want: 250},
			{raw: "", configured: true},
			{raw: "0", configured: true},
			{raw: "-1", configured: true},
			{raw: "1.5", configured: true},
			{raw: "bad", configured: true},
			{raw: "99999999999999999999999999", configured: true},
		} {
			t.Run(name+":"+test.raw, func(t *testing.T) {
				maximum, err := dailyLimitFromEnv(name, 100, func(key string) (string, bool) {
					if key != name {
						t.Fatalf("lookup %s; want %s", key, name)
					}
					return test.raw, test.configured
				})
				if maximum != test.want || (err != nil) != (test.want == 0) {
					t.Fatalf("limit = %d, %v; want %d", maximum, err, test.want)
				}
			})
		}
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
