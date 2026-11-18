//go:build e2e

package main

import (
	"net"
	"strings"
	"testing"
	"time"
)

func TestConfigureClockControlRequiresSafeExplicitLocalConfiguration(t *testing.T) {
	address := reserveLoopbackAddress(t)
	environment := validE2EClockEnvironment(address)
	control, err := configureClockControl(lookupMap(environment), clockControlSafety{
		AllowAuthEmulator: true,
		PushClient:        "dummy",
	})
	if err != nil {
		t.Fatal(err)
	}
	if control.Addr() != address || control.Clock().Now().Format(time.RFC3339Nano) != environment["LAST_ORDERS_E2E_INITIAL_TIME"] {
		t.Fatalf("configured control address/clock = %s / %s", control.Addr(), control.Clock().Now().Format(time.RFC3339Nano))
	}
	if err := control.Close(); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name    string
		changes map[string]string
		safety  clockControlSafety
	}{
		{name: "remote Firestore", changes: map[string]string{"FIRESTORE_EMULATOR_HOST": "firestore.example:8080"}},
		{name: "remote Auth", changes: map[string]string{"FIREBASE_AUTH_EMULATOR_HOST": "auth.example:9099"}},
		{name: "production project", changes: map[string]string{"GOOGLE_CLOUD_PROJECT": "production"}},
		{name: "credential file", changes: map[string]string{"GOOGLE_APPLICATION_CREDENTIALS": "service-account.json"}},
		{name: "credential JSON", changes: map[string]string{"LAST_ORDERS_FIREBASE_CREDENTIALS_JSON": "{}"}},
		{name: "live email delivery", changes: map[string]string{"MAILTRAP_TOKEN": "configured"}},
		{name: "live push delivery", safety: clockControlSafety{AllowAuthEmulator: true, PushClient: "webpush"}},
		{name: "auth emulator not allowed", safety: clockControlSafety{PushClient: "dummy"}},
		{name: "real auth deletion", safety: clockControlSafety{AllowAuthEmulator: true, PushClient: "dummy", EnableRealAuthDelete: true}},
		{name: "admin deletion enabled", changes: map[string]string{"ENABLE_ADMIN_DELETE_REQUESTS": "true"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := cloneEnvironment(environment)
			for key, value := range test.changes {
				candidate[key] = value
			}
			safety := test.safety
			if test.name != "live push delivery" && test.name != "auth emulator not allowed" && test.name != "real auth deletion" {
				safety = clockControlSafety{AllowAuthEmulator: true, PushClient: "dummy"}
			}
			if err := validateE2EClockSafety(lookupMap(candidate), safety); err == nil {
				t.Fatal("unsafe E2E configuration was accepted")
			}
		})
	}

	missingStart := cloneEnvironment(environment)
	delete(missingStart, "LAST_ORDERS_E2E_INITIAL_TIME")
	if _, err := configureClockControl(lookupMap(missingStart), clockControlSafety{AllowAuthEmulator: true, PushClient: "dummy"}); err == nil {
		t.Fatal("missing initial instant was accepted")
	}
	remoteControl := cloneEnvironment(environment)
	remoteControl["LAST_ORDERS_E2E_CLOCK_ADDR"] = "0.0.0.0:8082"
	if _, err := configureClockControl(lookupMap(remoteControl), clockControlSafety{AllowAuthEmulator: true, PushClient: "dummy"}); err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("non-loopback control listener error = %v", err)
	}
}

func validE2EClockEnvironment(address string) map[string]string {
	return map[string]string{
		"LAST_ORDERS_E2E_INITIAL_TIME": "2026-07-04T15:59:00+01:00",
		"LAST_ORDERS_E2E_CLOCK_ADDR":   address,
		"FIRESTORE_EMULATOR_HOST":      "127.0.0.1:8080",
		"FIREBASE_AUTH_EMULATOR_HOST":  "127.0.0.1:9099",
		"GOOGLE_CLOUD_PROJECT":         "demo-clock-test",
		"FIREBASE_AUTH_PROJECT_ID":     "demo-clock-test",
		"ADMIN_DELETE_DRY_RUN":         "true",
	}
}

func reserveLoopbackAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

func lookupMap(environment map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := environment[key]
		return value, ok
	}
}

func cloneEnvironment(environment map[string]string) map[string]string {
	clone := make(map[string]string, len(environment))
	for key, value := range environment {
		clone[key] = value
	}
	return clone
}
