//go:build !e2e

package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"last_orders/internal/lastorders/app"
)

func TestOrdinaryBuildRejectsE2EClockEnvironment(t *testing.T) {
	for _, key := range []string{"LAST_ORDERS_E2E_INITIAL_TIME", "LAST_ORDERS_E2E_CLOCK_ADDR"} {
		setup, err := configureClockControl(func(name string) (string, bool) {
			return "configured", name == key
		}, clockControlSafety{})
		if err == nil || setup != nil {
			t.Fatalf("clock setting %s: setup = %v, err = %v; want startup refusal", key, setup, err)
		}
	}
}

func TestOrdinaryBuildExcludesE2EClockAndControlSource(t *testing.T) {
	if app.E2EClockControlBuild {
		t.Fatal("ordinary build reports E2E clock control enabled")
	}
	for _, test := range []struct {
		packagePath string
		forbidden   string
	}{
		{packagePath: "last_orders/internal/lastorders/app", forbidden: "e2e_clock.go"},
		{packagePath: ".", forbidden: "e2e_clock_control.go"},
	} {
		command := exec.Command("go", "list", "-f", `{{join .GoFiles " "}}`, test.packagePath)
		command.Env = ordinaryGoEnvironment(os.Environ())
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("go list %s: %v\n%s", test.packagePath, err, output)
		}
		for _, source := range strings.Fields(string(output)) {
			if source == test.forbidden {
				t.Fatalf("ordinary package %s includes %s", test.packagePath, test.forbidden)
			}
		}
	}
}

func ordinaryGoEnvironment(environment []string) []string {
	ordinary := make([]string, 0, len(environment)+2)
	for _, entry := range environment {
		name, _, _ := strings.Cut(entry, "=")
		if strings.EqualFold(name, "GOFLAGS") || strings.EqualFold(name, "GOENV") {
			continue
		}
		ordinary = append(ordinary, entry)
	}
	return append(ordinary, "GOFLAGS=", "GOENV=off")
}
