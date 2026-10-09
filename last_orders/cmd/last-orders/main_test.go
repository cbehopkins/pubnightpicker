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

func TestMailtrapSandboxOptions(t *testing.T) {
	for _, raw := range []string{"", "123", " 123 ", "0", "-1", "bad", "1.5"} {
		t.Run(raw, func(t *testing.T) {
			opts, err := emailOptionsFromEnv(testLogger(), func(name string) string {
				switch name {
				case "MAILTRAP_TOKEN":
					return "live-token"
				case "MAILTRAP_SANDBOX_TOKEN":
					return "sandbox-token"
				case "MAILTRAP_SANDBOX_ID":
					return raw
				}
				return ""
			})
			valid := raw == "" || raw == "123" || raw == " 123 "
			if (err == nil) != valid {
				t.Fatalf("options = %+v, err = %v", opts, err)
			}
			if valid && opts.MailtrapSandboxToken != "sandbox-token" {
				t.Fatal("missing sandbox token")
			}
			if valid && raw != "" && opts.MailtrapSandboxID != 123 {
				t.Fatal("wrong sandbox ID")
			}
		})
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

func TestBoolFromEnv(t *testing.T) {
	for _, test := range []struct {
		name       string
		raw        string
		configured bool
		fallback   bool
		want       bool
		wantErr    bool
	}{
		{name: "default", fallback: true, want: true},
		{name: "enabled", raw: "true", configured: true, want: true},
		{name: "disabled", raw: "false", configured: true, want: false},
		{name: "whitespace", raw: " true ", configured: true, want: true},
		{name: "invalid", raw: "sometimes", configured: true, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := boolFromEnv("ADMIN_DELETE_SETTING", test.fallback, func(name string) (string, bool) {
				if name != "ADMIN_DELETE_SETTING" {
					t.Fatalf("lookup name = %q", name)
				}
				return test.raw, test.configured
			})
			if got != test.want || (err != nil) != test.wantErr {
				t.Fatalf("boolFromEnv() = %t, %v; want %t, error=%t", got, err, test.want, test.wantErr)
			}
		})
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
