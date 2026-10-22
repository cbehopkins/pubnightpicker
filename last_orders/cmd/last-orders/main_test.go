package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"log/slog"
	"strings"
	"testing"

	"cloud.google.com/go/firestore"
	emailplugin "last_orders/internal/lastorders/plugins/email"
)

func TestFirebaseConfigFromEnv(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keyBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	account := map[string]string{
		"type": "service_account", "project_id": "credential-project",
		"client_email": "backend@credential-project.iam.gserviceaccount.com",
		"private_key":  string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyBytes})),
		"token_uri":    "https://oauth2.googleapis.com/token",
	}
	encode := func(changes map[string]string) string {
		t.Helper()
		copyAccount := make(map[string]string, len(account))
		for name, value := range account {
			copyAccount[name] = value
		}
		for name, value := range changes {
			copyAccount[name] = value
		}
		data, err := json.Marshal(copyAccount)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	validJSON := encode(nil)
	const setting = "LAST_ORDERS_FIREBASE_CREDENTIALS_JSON"
	for _, test := range []struct {
		name        string
		env         map[string]string
		project     string
		authProject string
		credentials bool
		wantErr     bool
	}{
		{name: "defaults", project: firestore.DetectProjectID},
		{name: "file credentials", env: map[string]string{"GOOGLE_APPLICATION_CREDENTIALS": "existing.json"}, project: firestore.DetectProjectID},
		{name: "JSON", env: map[string]string{setting: validJSON}, project: "credential-project", authProject: "credential-project", credentials: true},
		{name: "overrides", env: map[string]string{setting: validJSON, "GOOGLE_CLOUD_PROJECT": "firestore-project", "FIREBASE_AUTH_PROJECT_ID": "auth-project"}, project: "firestore-project", authProject: "auth-project", credentials: true},
		{name: "project override", env: map[string]string{setting: validJSON, "GOOGLE_CLOUD_PROJECT": "override"}, project: "override", authProject: "override", credentials: true},
		{name: "emulator", env: map[string]string{"FIRESTORE_EMULATOR_HOST": "localhost:8080"}, project: "last-orders-emulator", authProject: "last-orders-emulator"},
		{name: "JSON with emulator", env: map[string]string{setting: validJSON, "FIRESTORE_EMULATOR_HOST": "localhost:8080"}, project: "last-orders-emulator", authProject: "last-orders-emulator", credentials: true},
		{name: "empty", env: map[string]string{setting: ""}, wantErr: true},
		{name: "whitespace", env: map[string]string{setting: "  "}, wantErr: true},
		{name: "malformed", env: map[string]string{setting: "secret-invalid-json"}, wantErr: true},
		{name: "wrong type", env: map[string]string{setting: encode(map[string]string{"type": "authorized_user"})}, wantErr: true},
		{name: "missing project", env: map[string]string{setting: encode(map[string]string{"project_id": ""})}, wantErr: true},
		{name: "missing email", env: map[string]string{setting: encode(map[string]string{"client_email": ""})}, wantErr: true},
		{name: "invalid key", env: map[string]string{setting: encode(map[string]string{"private_key": "secret-invalid-key"})}, wantErr: true},
		{name: "invalid PEM key", env: map[string]string{setting: encode(map[string]string{"private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("secret-invalid-key")}))})}, wantErr: true},
		{name: "conflict", env: map[string]string{setting: validJSON, "GOOGLE_APPLICATION_CREDENTIALS": "existing.json"}, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			config, err := firebaseConfigFromEnv(context.Background(), func(name string) (string, bool) {
				value, ok := test.env[name]
				return value, ok
			})
			if test.wantErr {
				if err == nil || !strings.Contains(err.Error(), setting) {
					t.Fatal("expected an error naming the credential setting")
				}
				for _, secret := range []string{"secret-invalid", account["private_key"], "backend@"} {
					if strings.Contains(err.Error(), secret) {
						t.Fatal("error contains credential material")
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if config.firestoreProjectID != test.project || config.authProjectID != test.authProject || (config.credentials != nil) != test.credentials {
				t.Fatal("unexpected project selection or credential presence")
			}
		})
	}
}

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
