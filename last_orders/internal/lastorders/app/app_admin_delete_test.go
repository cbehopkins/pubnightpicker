package app_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"last_orders/internal/lastorders/app"
	"last_orders/internal/lastorders/components/firebaseidempotency/firebaseidempotencytest"
	admindeletelistener "last_orders/internal/lastorders/database/listeners/admindelete"
	admindeletesvc "last_orders/internal/lastorders/services/admindelete"
)

type adminDeleteTestSource struct {
	mu      sync.Mutex
	status  string
	paused  bool
	pending []admindeletelistener.Document
}

func (s *adminDeleteTestSource) Watch(context.Context) (admindeletelistener.ChangeStream, error) {
	return nil, errors.New("not used by one-shot evaluation")
}

func (s *adminDeleteTestSource) WatchPause(context.Context) (admindeletelistener.PauseStream, error) {
	return nil, errors.New("not used by one-shot evaluation")
}

func (s *adminDeleteTestSource) ListEligible(_ context.Context, realDelete bool) ([]admindeletelistener.Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status != "pending" && !(realDelete && s.status == "dry_run_validated") {
		return nil, nil
	}
	for i := range s.pending {
		s.pending[i].Data["status"] = s.status
	}
	return s.pending, nil
}

func (s *adminDeleteTestSource) IsPaused(context.Context) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.paused, nil
}

func (s *adminDeleteTestSource) RequestStatus(context.Context, string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status, nil
}

func (s *adminDeleteTestSource) PersistOutcome(_ context.Context, payload admindeletesvc.OutcomePayload) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = string(payload.Outcome)
	return nil
}

type adminDeleteTestRepository struct {
	source *adminDeleteTestSource
}

func (r adminDeleteTestRepository) RequestStatus(ctx context.Context, requestID string) (string, bool, error) {
	status, err := r.source.RequestStatus(ctx, requestID)
	return status, status != "", err
}

func (r adminDeleteTestRepository) IsPaused(ctx context.Context) (bool, error) {
	return r.source.IsPaused(ctx)
}

func (adminDeleteTestRepository) ApplicationDataExists(context.Context, string) (bool, bool, error) {
	return false, false, nil
}

func (r adminDeleteTestRepository) PersistOutcome(ctx context.Context, payload admindeletesvc.OutcomePayload) error {
	return r.source.PersistOutcome(ctx, payload)
}

func (s *adminDeleteTestSource) ApplicationDataExists(context.Context, string) (bool, bool, error) {
	return false, false, nil
}

func TestAdminDeleteOneShotWaitsForPersistedOutcome(t *testing.T) {
	source := &adminDeleteTestSource{
		status: "pending",
		pending: []admindeletelistener.Document{{
			ID: "request-1",
			Data: map[string]any{
				"schemaVersion":    int64(1),
				"targetUid":        "target-1",
				"targetEmail":      nil,
				"requestedByUid":   "admin-1",
				"requestedByEmail": nil,
				"reason":           "admin_user_delete",
				"status":           "pending",
				"createdAt":        time.Now().UTC(),
			},
		}},
	}
	cfg := testConfig(t, filepath.Join(t.TempDir(), "admin-delete.db"), firebaseidempotencytest.NewInMemoryRemoteStandIn(false))
	cfg.AdminDeleteEnabled = true
	cfg.AdminDeleteDryRun = true
	cfg.AdminDeleteEvaluateOnce = true
	cfg.AdminDeleteSource = source
	cfg.AdminDeleteRepository = adminDeleteTestRepository{source: source}

	application, err := app.New(cfg)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer func() {
		if application != nil {
			application.Close()
		}
	}()
	if err := application.Run(ctx); err != nil {
		t.Fatalf("Run one-shot evaluation: %v", err)
	}
	status, err := source.RequestStatus(ctx, "request-1")
	if err != nil {
		t.Fatal(err)
	}
	if status != string(admindeletesvc.OutcomeDryRunValidated) {
		t.Fatalf("request status = %q, want %q", status, admindeletesvc.OutcomeDryRunValidated)
	}
	if err := application.Close(); err != nil {
		t.Fatal(err)
	}
	application = nil
	auth := &adminDeleteTestAuth{}
	cfg.AdminDeleteDryRun = false
	cfg.EnableRealAuthDelete = true
	cfg.AdminDeleteAuthClient = auth
	realApplication, err := app.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer realApplication.Close()
	if err := realApplication.Run(ctx); err != nil {
		t.Fatalf("real evaluation after dry-run: %v", err)
	}
	status, err = source.RequestStatus(ctx, "request-1")
	if err != nil {
		t.Fatal(err)
	}
	if status != string(admindeletesvc.OutcomeAuthDeleted) || auth.calls != 1 {
		t.Fatalf("real evaluation status/calls = %q/%d", status, auth.calls)
	}
	if err := realApplication.Run(ctx); err != nil {
		t.Fatalf("repeat real evaluation: %v", err)
	}
	if auth.calls != 1 {
		t.Fatalf("repeat real evaluation made %d Auth calls", auth.calls)
	}
}

type adminDeleteTestAuth struct {
	calls int
}

func (a *adminDeleteTestAuth) DeleteUser(context.Context, string) error {
	a.calls++
	return nil
}

func TestAdminDeleteOneShotRequiresFeatureEnablement(t *testing.T) {
	cfg := testConfig(t, filepath.Join(t.TempDir(), "disabled-admin-delete.db"), firebaseidempotencytest.NewInMemoryRemoteStandIn(false))
	cfg.AdminDeleteEvaluateOnce = true
	if _, err := app.New(cfg); err == nil {
		t.Fatal("app.New succeeded with one-shot mode but admin-delete is disabled")
	}
}
