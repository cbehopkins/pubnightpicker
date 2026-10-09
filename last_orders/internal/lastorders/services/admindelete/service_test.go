package admindelete

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"cellar/pkg/cellar"
	"google.golang.org/api/googleapi"
	"last_orders/internal/lastorders/truths"
)

type fakeRepository struct {
	status       string
	exists       bool
	paused       bool
	usersExists  bool
	publicExists bool
	persisted    []OutcomePayload
}

func (r *fakeRepository) RequestStatus(context.Context, string) (string, bool, error) {
	return r.status, r.exists, nil
}

func (r *fakeRepository) IsPaused(context.Context) (bool, error) {
	return r.paused, nil
}

func (r *fakeRepository) ApplicationDataExists(context.Context, string) (bool, bool, error) {
	return r.usersExists, r.publicExists, nil
}

func (r *fakeRepository) PersistOutcome(_ context.Context, payload OutcomePayload) error {
	r.persisted = append(r.persisted, payload)
	return nil
}

type fakeAuth struct {
	calls int
	err   error
}

func (a *fakeAuth) DeleteUser(context.Context, string) error {
	a.calls++
	return a.err
}

func TestEvaluatorValidatesRequestBeforeAuthAndPersistsDryRun(t *testing.T) {
	repository := &fakeRepository{status: "pending", exists: true}
	auth := &fakeAuth{}
	evaluator := newEvaluator(t, Options{
		Enabled: true, DryRun: true, EnableRealAuthDelete: true,
		Repository: repository, Auth: auth,
	})
	result := evaluator.Handle(context.Background(), validTruth())
	payload := persistencePayload(t, result)
	if payload.Outcome != OutcomeDryRunValidated {
		t.Fatalf("outcome = %q, want %q", payload.Outcome, OutcomeDryRunValidated)
	}
	if auth.calls != 0 {
		t.Fatalf("Auth calls = %d, want 0 in dry-run", auth.calls)
	}
}

func TestEvaluatorPromotesDryRunOnlyWithRealDeleteGatesAndFreshChecks(t *testing.T) {
	for _, test := range []struct {
		name                                                  string
		dryRun, capability, paused, usersExists, publicExists bool
		want                                                  Outcome
		calls                                                 int
	}{
		{name: "real delete", capability: true, want: OutcomeAuthDeleted, calls: 1},
		{name: "dry run stays validated", dryRun: true, capability: true},
		{name: "no capability stays validated"},
		{name: "pause retains request", capability: true, paused: true},
		{name: "recheck users", capability: true, usersExists: true, want: OutcomeFailedPrecondition},
		{name: "recheck public", capability: true, publicExists: true, want: OutcomeFailedPrecondition},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &fakeRepository{status: string(OutcomeDryRunValidated), exists: true,
				paused: test.paused, usersExists: test.usersExists, publicExists: test.publicExists}
			auth := &fakeAuth{}
			evaluator := newEvaluator(t, Options{Enabled: true, DryRun: test.dryRun,
				EnableRealAuthDelete: test.capability, Repository: repository, Auth: auth})
			result := evaluator.Handle(context.Background(), validTruth())
			if test.paused {
				if _, ok := result.(cellar.RetrySequence); !ok {
					t.Fatalf("paused result = %#v", result)
				}
			} else if test.want != "" {
				payload := persistencePayload(t, result)
				if payload.Outcome != test.want || !payload.RealDelete {
					t.Fatalf("payload = %+v", payload)
				}
			} else if complete, ok := result.(cellar.Complete); !ok || len(complete.NewCells) != 0 {
				t.Fatalf("ineligible result = %#v", result)
			}
			if auth.calls != test.calls {
				t.Fatalf("Auth calls = %d, want %d", auth.calls, test.calls)
			}
		})
	}
}

func TestEvaluatorRejectsUnsupportedSchemaWithoutAuth(t *testing.T) {
	repository := &fakeRepository{status: "pending", exists: true}
	auth := &fakeAuth{}
	evaluator := newEvaluator(t, Options{Enabled: true, DryRun: true, Repository: repository, Auth: auth})
	request := validTruth()
	request.Request.SchemaVersion = 2
	payload := persistencePayload(t, evaluator.Handle(context.Background(), request))
	if payload.Outcome != OutcomeInvalidRequest {
		t.Fatalf("outcome = %q, want %q", payload.Outcome, OutcomeInvalidRequest)
	}
	if auth.calls != 0 {
		t.Fatalf("Auth calls = %d, want 0", auth.calls)
	}
}

func TestEvaluatorDoesNotDeleteWhileApplicationDataExists(t *testing.T) {
	for _, test := range []struct {
		name         string
		usersExists  bool
		publicExists bool
	}{
		{name: "users document", usersExists: true},
		{name: "public document", publicExists: true},
		{name: "both documents", usersExists: true, publicExists: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &fakeRepository{status: "pending", exists: true, usersExists: test.usersExists, publicExists: test.publicExists}
			auth := &fakeAuth{}
			evaluator := newEvaluator(t, Options{Enabled: true, Repository: repository, Auth: auth})
			payload := persistencePayload(t, evaluator.Handle(context.Background(), validTruth()))
			if payload.Outcome != OutcomeFailedPrecondition {
				t.Fatalf("outcome = %q, want %q", payload.Outcome, OutcomeFailedPrecondition)
			}
			if payload.UsersDocExists != test.usersExists || payload.UserPublicDocExists != test.publicExists {
				t.Fatalf("precondition evidence = %t/%t", payload.UsersDocExists, payload.UserPublicDocExists)
			}
			if auth.calls != 0 {
				t.Fatalf("Auth calls = %d, want 0", auth.calls)
			}
		})
	}
}

func TestEvaluatorBlocksRealDeleteWithoutCapability(t *testing.T) {
	repository := &fakeRepository{status: "pending", exists: true}
	auth := &fakeAuth{}
	evaluator := newEvaluator(t, Options{Enabled: true, Repository: repository, Auth: auth})
	payload := persistencePayload(t, evaluator.Handle(context.Background(), validTruth()))
	if payload.Outcome != OutcomeDeleteBlocked {
		t.Fatalf("outcome = %q, want %q", payload.Outcome, OutcomeDeleteBlocked)
	}
	if auth.calls != 0 {
		t.Fatalf("Auth calls = %d, want 0", auth.calls)
	}
}

func TestEvaluatorTreatsUserNotFoundAsSuccessfulConvergence(t *testing.T) {
	repository := &fakeRepository{status: "pending", exists: true}
	auth := &fakeAuth{err: &googleapi.Error{Code: http.StatusNotFound}}
	evaluator := newEvaluator(t, Options{Enabled: true, EnableRealAuthDelete: true, Repository: repository, Auth: auth})
	payload := persistencePayload(t, evaluator.Handle(context.Background(), validTruth()))
	if payload.Outcome != OutcomeAuthDeleted || !payload.Idempotent {
		t.Fatalf("payload = %+v, want idempotent auth_deleted", payload)
	}
	if auth.calls != 1 {
		t.Fatalf("Auth calls = %d, want 1", auth.calls)
	}
}

func TestEvaluatorPersistsSuccessfulAuthDelete(t *testing.T) {
	repository := &fakeRepository{status: "pending", exists: true}
	auth := &fakeAuth{}
	evaluator := newEvaluator(t, Options{Enabled: true, EnableRealAuthDelete: true, Repository: repository, Auth: auth})
	payload := persistencePayload(t, evaluator.Handle(context.Background(), validTruth()))
	if payload.Outcome != OutcomeAuthDeleted || payload.Idempotent {
		t.Fatalf("payload = %+v, want non-idempotent auth_deleted", payload)
	}
}

func TestEvaluatorRetriesUncertainAuthFailures(t *testing.T) {
	repository := &fakeRepository{status: "pending", exists: true}
	auth := &fakeAuth{err: errors.New("connection reset")}
	evaluator := newEvaluator(t, Options{Enabled: true, EnableRealAuthDelete: true, Repository: repository, Auth: auth})
	result := evaluator.Handle(context.Background(), validTruth())
	retry, ok := result.(cellar.RetrySequence)
	if !ok || retry.Delay <= 0 {
		t.Fatalf("result = %#v, want delayed RetrySequence", result)
	}
}

func TestEvaluatorPersistsPermanentAuthFailure(t *testing.T) {
	repository := &fakeRepository{status: "pending", exists: true}
	auth := &fakeAuth{err: &googleapi.Error{Code: http.StatusForbidden, Message: "permission denied"}}
	evaluator := newEvaluator(t, Options{Enabled: true, EnableRealAuthDelete: true, Repository: repository, Auth: auth})
	payload := persistencePayload(t, evaluator.Handle(context.Background(), validTruth()))
	if payload.Outcome != OutcomeAuthDeleteFailed || payload.Error == "" {
		t.Fatalf("payload = %+v, want terminal auth_delete_failed with error", payload)
	}
}

func TestEvaluatorRetriesWhilePausedAndIgnoresTerminalRequest(t *testing.T) {
	repository := &fakeRepository{status: "pending", exists: true, paused: true}
	evaluator := newEvaluator(t, Options{Enabled: true, Repository: repository})
	result := evaluator.Handle(context.Background(), validTruth())
	if _, ok := result.(cellar.RetrySequence); !ok {
		t.Fatalf("paused result = %#v, want RetrySequence", result)
	}

	repository.paused = false
	repository.status = string(OutcomeAuthDeleted)
	result = evaluator.Handle(context.Background(), validTruth())
	if _, ok := result.(cellar.Complete); !ok {
		t.Fatalf("terminal request result = %#v, want Complete", result)
	}
}

func TestPersistenceHandlerRetriesRepositoryErrors(t *testing.T) {
	repository := &errorRepository{fakeRepository: fakeRepository{status: "pending", exists: true}}
	handler := PersistenceHandler{Repository: repository}
	result := handler.Handle(context.Background(), OutcomePayload{RequestID: "request-1", Outcome: OutcomeAuthDeleted})
	if _, ok := result.(cellar.RetrySequence); !ok {
		t.Fatalf("result = %#v, want RetrySequence", result)
	}
}

type errorRepository struct {
	fakeRepository
}

func (r *errorRepository) PersistOutcome(context.Context, OutcomePayload) error {
	return errors.New("firestore unavailable")
}

func newEvaluator(t *testing.T, options Options) *Evaluator {
	t.Helper()
	options.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	evaluator, err := New(options)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return evaluator
}

func validTruth() truths.AdminDeleteRequested {
	return truths.AdminDeleteRequested{Request: truths.AdminDeleteRequestSnapshot{
		RequestID:        "request-1",
		TargetUID:        "target-1",
		TargetEmail:      "",
		RequestedByUID:   "admin-1",
		RequestedByEmail: "",
		Reason:           "admin_user_delete",
		SchemaVersion:    1,
		CreatedAt:        time.Now().UTC(),
	}}
}

func persistencePayload(t *testing.T, result cellar.Result) OutcomePayload {
	t.Helper()
	completed, ok := result.(cellar.Complete)
	if !ok || len(completed.NewCells) != 1 {
		t.Fatalf("result = %#v, want Complete with one persistence Cell", result)
	}
	var payload OutcomePayload
	if err := json.Unmarshal(completed.NewCells[0].Steps[0].Payload, &payload); err != nil {
		t.Fatalf("decode persistence payload: %v", err)
	}
	return payload
}
