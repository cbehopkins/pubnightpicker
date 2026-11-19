//go:build e2e

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/app"
)

type e2eClockControl struct {
	clock    *app.E2EClock
	listener net.Listener
	server   *http.Server
}

func configureClockControl(lookup func(string) (string, bool), safety clockControlSafety) (clockControlSetup, error) {
	initialText, configured := lookup("LAST_ORDERS_E2E_INITIAL_TIME")
	if !configured || strings.TrimSpace(initialText) == "" {
		return nil, errors.New("LAST_ORDERS_E2E_INITIAL_TIME must be an explicit RFC3339 instant")
	}
	initial, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(initialText))
	if err != nil {
		return nil, errors.New("LAST_ORDERS_E2E_INITIAL_TIME must be an explicit RFC3339 instant with an offset")
	}
	address, configured := lookup("LAST_ORDERS_E2E_CLOCK_ADDR")
	if !configured || strings.TrimSpace(address) == "" {
		return nil, errors.New("LAST_ORDERS_E2E_CLOCK_ADDR must be an explicit loopback address")
	}
	if err := validateE2EClockSafety(lookup, safety); err != nil {
		return nil, err
	}
	listener, err := listenE2EClock(address)
	if err != nil {
		return nil, err
	}
	clock, err := app.NewE2EClock(initial)
	if err != nil {
		_ = listener.Close()
		return nil, err
	}
	return &e2eClockControl{clock: clock, listener: listener}, nil
}

func validateE2EClockSafety(lookup func(string) (string, bool), safety clockControlSafety) error {
	for _, key := range []string{"FIRESTORE_EMULATOR_HOST", "FIREBASE_AUTH_EMULATOR_HOST"} {
		address, configured := lookup(key)
		if !configured || !isLoopbackAddress(address) {
			return fmt.Errorf("%s must identify a local emulator", key)
		}
	}
	project, configured := lookup("GOOGLE_CLOUD_PROJECT")
	project = strings.TrimSpace(project)
	if !configured || !strings.HasPrefix(project, "demo-") || len(project) == len("demo-") {
		return errors.New("GOOGLE_CLOUD_PROJECT must use a demo project in E2E clock mode")
	}
	authProject, configured := lookup("FIREBASE_AUTH_PROJECT_ID")
	if !configured || strings.TrimSpace(authProject) != project {
		return errors.New("FIREBASE_AUTH_PROJECT_ID must match the demo Firestore project")
	}
	for _, key := range []string{"GOOGLE_APPLICATION_CREDENTIALS", "LAST_ORDERS_FIREBASE_CREDENTIALS_JSON"} {
		if _, configured := lookup(key); configured {
			return fmt.Errorf("%s must be unset in E2E clock mode", key)
		}
	}
	for _, key := range []string{
		"MAILTRAP_TOKEN", "MAILTRAP_SANDBOX_TOKEN", "MAILTRAP_SANDBOX_ID",
		"SWEEGO_TOKEN", "SWEEGO_PROVIDER", "SWEEGO_BASE_URL",
		"WEB_PUSH_VAPID_PRIVATE_KEY", "WEB_PUSH_VAPID_SUBJECT",
	} {
		if value, configured := lookup(key); configured && strings.TrimSpace(value) != "" {
			return fmt.Errorf("%s must be unset for dummy delivery in E2E clock mode", key)
		}
	}
	if !safety.AllowAuthEmulator {
		return errors.New("-allow-auth-emulator is required in E2E clock mode")
	}
	if safety.PushClient != "dummy" {
		return errors.New("-push-client=dummy is required in E2E clock mode")
	}
	if safety.EnableRealAuthDelete {
		return errors.New("-enable-real-auth-delete is forbidden in E2E clock mode")
	}
	adminDeleteEnabled, err := boolFromEnv("ENABLE_ADMIN_DELETE_REQUESTS", false, lookup)
	if err != nil || adminDeleteEnabled {
		return errors.New("ENABLE_ADMIN_DELETE_REQUESTS must be false in E2E clock mode")
	}
	adminDeleteDryRun, err := boolFromEnv("ADMIN_DELETE_DRY_RUN", true, lookup)
	if err != nil || !adminDeleteDryRun {
		return errors.New("ADMIN_DELETE_DRY_RUN must be true in E2E clock mode")
	}
	return nil
}

func isLoopbackAddress(address string) bool {
	host, portText, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	port, err := strconv.Atoi(portText)
	return ip != nil && ip.IsLoopback() && err == nil && port > 0 && port <= 65535
}

func listenE2EClock(address string) (net.Listener, error) {
	if !isLoopbackAddress(address) {
		return nil, errors.New("LAST_ORDERS_E2E_CLOCK_ADDR must be an explicit loopback IP and port")
	}
	listener, err := net.Listen("tcp", strings.TrimSpace(address))
	if err != nil {
		return nil, fmt.Errorf("listen for E2E clock control: %w", err)
	}
	return listener, nil
}

func (control *e2eClockControl) Clock() cellar.Clock {
	return control.clock
}

func (control *e2eClockControl) Start(application *app.App, logger *slog.Logger) error {
	if control.server != nil {
		return errors.New("E2E clock control is already running")
	}
	control.server = &http.Server{
		Handler:           app.E2EClockControlHandler(control.clock, application.NotifyScheduling),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    8 * 1024,
	}
	go func() {
		if err := control.server.Serve(control.listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("E2E clock control stopped unexpectedly", "err", err)
		}
	}()
	return nil
}

func (control *e2eClockControl) Close() error {
	if control.server == nil {
		return control.listener.Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := control.server.Shutdown(ctx); err != nil {
		_ = control.server.Close()
		return fmt.Errorf("shutdown E2E clock control: %w", err)
	}
	return nil
}

func (control *e2eClockControl) Addr() string {
	return control.listener.Addr().String()
}
