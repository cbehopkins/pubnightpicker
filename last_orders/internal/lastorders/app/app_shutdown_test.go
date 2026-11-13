package app_test

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/app"
	"last_orders/internal/lastorders/components/firebaseidempotency"
	"last_orders/internal/lastorders/components/firebaseidempotency/firebaseidempotencytest"
	"last_orders/internal/lastorders/components/recurrence"
	"last_orders/internal/lastorders/components/recurrence/recurrencetest"
	eventvenues "last_orders/internal/lastorders/database/listeners/eventvenues"
	"last_orders/internal/lastorders/database/listeners/eventvenues/eventvenuestest"
	"last_orders/internal/lastorders/database/listeners/newpolls"
	recurrenceplugin "last_orders/internal/lastorders/plugins/recurrence"
	"last_orders/internal/lastorders/truths"
)

type blockingEventPollCreator struct {
	delegate *recurrencetest.Service
	started  chan struct{}
	release  chan struct{}
}

type stagedDrainEventPollCreator struct {
	delegate *recurrencetest.Service
	started  chan struct{}
	release  chan struct{}
	calls    atomic.Int32
}

type blockingHasKeyRemote struct {
	delegate   firebaseidempotency.Remote
	started    chan struct{}
	release    chan struct{}
	startOnce  sync.Once
	createKeys atomic.Int32
}

type countingRemote struct {
	delegate   firebaseidempotency.Remote
	createKeys atomic.Int32
}

type failedReleaseRemote struct {
	delegate firebaseidempotency.Remote
	err      error
}

type ambiguousClaimRemote struct {
	createStarted chan struct{}
	releaseCreate chan struct{}
	lookupCalled  chan struct{}
	createKeys    atomic.Int32
	owner         string
	ownerOverride string
	lookupExists  bool
	createErr     error
	lookupErr     error
	waitForCancel bool
	lookupOnce    sync.Once
}

func (r *countingRemote) HasKey(ctx context.Context, listener, eventKey string) (bool, error) {
	return r.delegate.HasKey(ctx, listener, eventKey)
}

func (r *countingRemote) LookupOwner(ctx context.Context, listener, eventKey string) (string, bool, error) {
	return r.delegate.LookupOwner(ctx, listener, eventKey)
}

func (r *countingRemote) ReleaseKey(ctx context.Context, listener, eventKey, owner string) (bool, error) {
	return r.delegate.ReleaseKey(ctx, listener, eventKey, owner)
}

func (r *failedReleaseRemote) CreateKey(ctx context.Context, listener, eventKey, owner string) (bool, bool, error) {
	return r.delegate.CreateKey(ctx, listener, eventKey, owner)
}

func (r *failedReleaseRemote) HasKey(ctx context.Context, listener, eventKey string) (bool, error) {
	return r.delegate.HasKey(ctx, listener, eventKey)
}

func (r *failedReleaseRemote) LookupOwner(ctx context.Context, listener, eventKey string) (string, bool, error) {
	return r.delegate.LookupOwner(ctx, listener, eventKey)
}

func (r *failedReleaseRemote) ReleaseKey(context.Context, string, string, string) (bool, error) {
	return false, r.err
}

func (r *countingRemote) CreateKey(ctx context.Context, listener, eventKey, owner string) (bool, bool, error) {
	r.createKeys.Add(1)
	return r.delegate.CreateKey(ctx, listener, eventKey, owner)
}

func (r *ambiguousClaimRemote) HasKey(context.Context, string, string) (bool, error) {
	return false, nil
}

func (r *ambiguousClaimRemote) CreateKey(ctx context.Context, _, _, owner string) (bool, bool, error) {
	r.createKeys.Add(1)
	r.owner = owner
	close(r.createStarted)
	if r.waitForCancel {
		<-ctx.Done()
		return false, false, ctx.Err()
	}
	<-r.releaseCreate
	if r.createErr != nil {
		return false, false, r.createErr
	}
	return false, true, nil
}

func (r *ambiguousClaimRemote) LookupOwner(context.Context, string, string) (string, bool, error) {
	r.lookupOnce.Do(func() { close(r.lookupCalled) })
	if r.lookupErr != nil {
		return "", false, r.lookupErr
	}
	if !r.lookupExists {
		return "", false, nil
	}
	if r.ownerOverride != "" {
		return r.ownerOverride, true, nil
	}
	return r.owner, true, nil
}

func (r *ambiguousClaimRemote) ReleaseKey(context.Context, string, string, string) (bool, error) {
	return false, nil
}

type shutdownEventVenueSource struct {
	started chan struct{}
	release chan struct{}
	stopped chan struct{}
}

func (s *shutdownEventVenueSource) Watch(ctx context.Context) (eventvenues.ChangeStream, error) {
	return &shutdownEventVenueStream{
		ctx:     ctx,
		started: s.started,
		release: s.release,
		stopped: s.stopped,
		venue: recurrence.EventVenue{
			ID:                 "shutdown-listener-event",
			Name:               "Shutdown event",
			NextOccurrenceDate: "2026-11-13",
		},
	}, nil
}

func (s *shutdownEventVenueSource) ListEventVenues(context.Context) ([]recurrence.EventVenue, error) {
	return nil, nil
}

type shutdownEventVenueStream struct {
	ctx         context.Context
	started     chan struct{}
	release     chan struct{}
	stopped     chan struct{}
	venue       recurrence.EventVenue
	first       bool
	startedOnce sync.Once
	stoppedOnce sync.Once
}

type liveNewPollSource struct {
	mu            sync.Mutex
	documents     map[string]newpolls.Document
	watchStarted  chan struct{}
	streamStopped chan struct{}
	watchOnce     sync.Once
	stopOnce      sync.Once
}

type liveNewPollStream struct {
	ctx       context.Context
	changes   []newpolls.Change
	delivered bool
	source    *liveNewPollSource
}

type materializingDrainEventPollCreator struct {
	delegate  *recurrencetest.Service
	source    *liveNewPollSource
	started   chan struct{}
	release   chan struct{}
	startOnce sync.Once
}

func (s *shutdownEventVenueStream) Next() ([]eventvenues.Change, error) {
	if !s.first {
		s.first = true
		s.startedOnce.Do(func() { close(s.started) })
		<-s.release
		return []eventvenues.Change{{Kind: eventvenues.ChangeAdded, Venue: s.venue}}, nil
	}
	<-s.ctx.Done()
	return nil, s.ctx.Err()
}

func (s *shutdownEventVenueStream) Stop() {
	s.stoppedOnce.Do(func() { close(s.stopped) })
}

func newLiveNewPollSource() *liveNewPollSource {
	return &liveNewPollSource{
		documents:     make(map[string]newpolls.Document),
		watchStarted:  make(chan struct{}),
		streamStopped: make(chan struct{}),
	}
}

func (s *liveNewPollSource) Watch(ctx context.Context) (newpolls.ChangeStream, error) {
	s.mu.Lock()
	changes := make([]newpolls.Change, 0, len(s.documents))
	for _, document := range s.documents {
		changes = append(changes, newpolls.Change{Kind: newpolls.ChangeAdded, Doc: document})
	}
	s.mu.Unlock()
	s.watchOnce.Do(func() { close(s.watchStarted) })
	return &liveNewPollStream{ctx: ctx, changes: changes, source: s}, nil
}

func (s *liveNewPollSource) AddPoll(pollID string) {
	s.mu.Lock()
	s.documents[pollID] = newpolls.Document{ID: pollID, Data: map[string]any{"date": "2026-11-13"}}
	s.mu.Unlock()
}

func (s *liveNewPollStream) Next() ([]newpolls.Change, error) {
	if !s.delivered {
		s.delivered = true
		if len(s.changes) > 0 {
			return s.changes, nil
		}
	}
	<-s.ctx.Done()
	return nil, s.ctx.Err()
}

func (s *liveNewPollStream) Stop() {
	s.source.stopOnce.Do(func() { close(s.source.streamStopped) })
}

func (s *materializingDrainEventPollCreator) Location() *time.Location {
	return s.delegate.Location()
}

func (s *materializingDrainEventPollCreator) Today() time.Time {
	return s.delegate.Today()
}

func (s *materializingDrainEventPollCreator) AdvanceStaleEvent(ctx context.Context, eventID string) error {
	return s.delegate.AdvanceStaleEvent(ctx, eventID)
}

func (s *materializingDrainEventPollCreator) CreateEventPoll(ctx context.Context, eventID, occurrenceDate string) error {
	s.startOnce.Do(func() { close(s.started) })
	select {
	case <-s.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	s.source.AddPoll(eventID)
	return nil
}

func (r *blockingHasKeyRemote) HasKey(ctx context.Context, listener, eventKey string) (bool, error) {
	r.startOnce.Do(func() { close(r.started) })
	select {
	case <-r.release:
	case <-ctx.Done():
		return false, ctx.Err()
	}
	return r.delegate.HasKey(ctx, listener, eventKey)
}

func (r *blockingHasKeyRemote) LookupOwner(ctx context.Context, listener, eventKey string) (string, bool, error) {
	return r.delegate.LookupOwner(ctx, listener, eventKey)
}

func (r *blockingHasKeyRemote) ReleaseKey(ctx context.Context, listener, eventKey, owner string) (bool, error) {
	return r.delegate.ReleaseKey(ctx, listener, eventKey, owner)
}

func (r *blockingHasKeyRemote) CreateKey(ctx context.Context, listener, eventKey, owner string) (bool, bool, error) {
	r.createKeys.Add(1)
	return r.delegate.CreateKey(ctx, listener, eventKey, owner)
}

func (s blockingEventPollCreator) Location() *time.Location {
	return s.delegate.Location()
}

func (s blockingEventPollCreator) Today() time.Time {
	return s.delegate.Today()
}

func (s blockingEventPollCreator) AdvanceStaleEvent(ctx context.Context, eventID string) error {
	return s.delegate.AdvanceStaleEvent(ctx, eventID)
}

func (s blockingEventPollCreator) CreateEventPoll(ctx context.Context, eventID, occurrenceDate string) error {
	close(s.started)
	select {
	case <-s.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

func (s *stagedDrainEventPollCreator) Location() *time.Location {
	return s.delegate.Location()
}

func (s *stagedDrainEventPollCreator) Today() time.Time {
	return s.delegate.Today()
}

func (s *stagedDrainEventPollCreator) AdvanceStaleEvent(ctx context.Context, eventID string) error {
	return s.delegate.AdvanceStaleEvent(ctx, eventID)
}

func (s *stagedDrainEventPollCreator) CreateEventPoll(ctx context.Context, eventID, occurrenceDate string) error {
	if s.calls.Add(1) == 1 {
		close(s.started)
		select {
		case <-s.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func TestShutdownDrainCompletesConfirmedCell(t *testing.T) {
	t.Parallel()

	recurrence, err := recurrencetest.New(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	creator := blockingEventPollCreator{
		delegate: recurrence,
		started:  make(chan struct{}),
		release:  make(chan struct{}),
	}
	cfg := testConfig(t, filepath.Join(t.TempDir(), "shutdown-drain.db"), firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
	cfg.RecurrenceService = creator
	cfg.ShutdownDrainTimeout = 5 * time.Second
	a := newApp(t, cfg)
	defer a.Close()
	if err := addShutdownDrainCell(t, a); err != nil {
		t.Fatalf("add confirmed cell: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	select {
	case <-creator.started:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("confirmed cell did not start")
	}

	cancel()
	close(creator.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run app: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("app did not finish its shutdown drain")
	}
	if active := activeWorkCells(t, a.CellarStore()); len(active) != 0 {
		t.Fatalf("shutdown left confirmed work unfinished: %+v", active)
	}
	if err := a.AddCell(cellar.CellRequest{}); !errors.Is(err, app.ErrAppShuttingDown) {
		t.Fatalf("AddCell() after shutdown = %v, want ErrAppShuttingDown", err)
	}
	all, err := a.CellarStore().ListActive()
	if err != nil {
		t.Fatal(err)
	}
	timerCells := 0
	for _, cell := range all {
		if strings.HasPrefix(string(cell.ID), "timer:") {
			timerCells++
			if cell.State != cellar.CellStateReady {
				t.Fatalf("timer cell state = %s, want READY", cell.State)
			}
		}
	}
	if timerCells == 0 {
		t.Fatal("shutdown removed durable timer work")
	}
}

func TestShutdownDrainRunsRetryThatBecomesDueWhileWorkerIsActive(t *testing.T) {
	t.Parallel()

	recurrence, err := recurrencetest.New(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	creator := &stagedDrainEventPollCreator{
		delegate: recurrence,
		started:  make(chan struct{}),
		release:  make(chan struct{}),
	}
	logs := &syncBuffer{}
	cfg := testConfig(t, filepath.Join(t.TempDir(), "shutdown-due-retry.db"), firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
	cfg.Logger = slog.New(slog.NewJSONHandler(logs, nil))
	cfg.RecurrenceService = creator
	cfg.ShutdownDrainTimeout = 5 * time.Second
	a := newApp(t, cfg)
	defer a.Close()

	retryDefinition, err := cellar.NewCellDefinition(recurrenceplugin.HandlerCreateEventPoll, truths.CreateEventPoll{
		EventID:        "shutdown-due-retry-event",
		OccurrenceDate: "2026-11-13",
	})
	if err != nil {
		t.Fatal(err)
	}
	retryCell, err := retryDefinition.CellRequest()
	if err != nil {
		t.Fatal(err)
	}
	retryCell.ID = "shutdown-due-retry"
	if err := a.AddCell(retryCell); err != nil {
		t.Fatal(err)
	}
	drainStore, ok := a.CellarStore().(cellar.DrainSchedulerStore)
	if !ok {
		t.Fatal("Cellar store does not support filtered claiming")
	}
	retried, claimed, err := drainStore.ClaimNextMatching(time.Now(), func(cell cellar.Cell) bool {
		return cell.ID == retryCell.ID
	})
	if err != nil || !claimed {
		t.Fatalf("claim cell to persist retry result: claimed=%t err=%v", claimed, err)
	}
	resultStore, ok := a.CellarStore().(cellar.ResultStore)
	if !ok {
		t.Fatal("Cellar store does not support result application")
	}
	notBefore := time.Now().Add(time.Second)
	if err := resultStore.ApplyResult(retried, cellar.Retry{NotBefore: &notBefore}); err != nil {
		t.Fatalf("persist future retry: %v", err)
	}
	if err := addShutdownDrainCell(t, a); err != nil {
		t.Fatalf("add active drain cell: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	select {
	case <-creator.started:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("active drain cell did not start")
	}
	cancel()
	waitForDrainStarted(t, logs)
	if wait := time.Until(notBefore); wait > 0 {
		timer := time.NewTimer(wait)
		<-timer.C
	}
	close(creator.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run app: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("app did not finish its shutdown drain")
	}
	if calls := creator.calls.Load(); calls != 2 {
		t.Fatalf("event poll executions = %d, want active work then newly due retry", calls)
	}
}

func TestShutdownDrainLeavesFutureRetryForSameDatabaseRestart(t *testing.T) {
	t.Parallel()

	recurrence, err := recurrencetest.New(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(t.TempDir(), "shutdown-restart-retry.db")
	firstCreator := &stagedDrainEventPollCreator{
		delegate: recurrence,
		started:  make(chan struct{}),
		release:  make(chan struct{}),
	}
	firstConfig := testConfig(t, dbPath, firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
	firstConfig.RecurrenceService = firstCreator
	firstConfig.ShutdownDrainTimeout = 5 * time.Second
	first := newApp(t, firstConfig)
	defer first.Close()
	close(firstCreator.release)

	definition, err := cellar.NewCellDefinition(recurrenceplugin.HandlerCreateEventPoll, truths.CreateEventPoll{
		EventID:        "shutdown-restart-retry-event",
		OccurrenceDate: "2026-11-13",
	})
	if err != nil {
		t.Fatal(err)
	}
	retryCell, err := definition.CellRequest()
	if err != nil {
		t.Fatal(err)
	}
	retryCell.ID = "shutdown-restart-retry"
	if err := first.AddCell(retryCell); err != nil {
		t.Fatal(err)
	}
	drainStore, ok := first.CellarStore().(cellar.DrainSchedulerStore)
	if !ok {
		t.Fatal("Cellar store does not support filtered claiming")
	}
	claimedCell, claimed, err := drainStore.ClaimNextMatching(time.Now(), func(cell cellar.Cell) bool {
		return cell.ID == retryCell.ID
	})
	if err != nil || !claimed {
		t.Fatalf("claim cell to persist retry result: claimed=%t err=%v", claimed, err)
	}
	resultStore, ok := first.CellarStore().(cellar.ResultStore)
	if !ok {
		t.Fatal("Cellar store does not support result application")
	}
	notBefore := time.Now().Add(2 * time.Second)
	if err := resultStore.ApplyResult(claimedCell, cellar.Retry{NotBefore: &notBefore}); err != nil {
		t.Fatalf("persist future retry: %v", err)
	}
	if err := addShutdownDrainCell(t, first); err != nil {
		t.Fatalf("add startup handshake cell: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- first.Run(ctx) }()
	select {
	case <-firstCreator.started:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("startup handshake work did not execute")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run first app: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first app did not finish its shutdown drain")
	}
	active := activeWorkCells(t, first.CellarStore())
	if len(active) != 1 || active[0].NotBefore == nil || !active[0].NotBefore.Equal(notBefore) {
		t.Fatalf("future retry was not preserved at shutdown: %+v", active)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first app: %v", err)
	}

	if wait := time.Until(notBefore); wait > 0 {
		timer := time.NewTimer(wait)
		<-timer.C
	}
	secondCreator := &stagedDrainEventPollCreator{
		delegate: recurrence,
		started:  make(chan struct{}),
		release:  make(chan struct{}),
	}
	close(secondCreator.release)
	secondConfig := testConfig(t, dbPath, firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
	secondConfig.RecurrenceService = secondCreator
	second := newApp(t, secondConfig)
	defer second.Close()
	secondCtx, stopSecond := context.WithCancel(context.Background())
	secondDone := make(chan error, 1)
	go func() { secondDone <- second.Run(secondCtx) }()
	select {
	case <-secondCreator.started:
	case <-time.After(5 * time.Second):
		stopSecond()
		t.Fatal("same-database restart did not run the due retry")
	}
	stopSecond()
	if err := <-secondDone; err != nil {
		t.Fatalf("run restarted app: %v", err)
	}
}

func TestShutdownDrainRelinquishesFreshClaim(t *testing.T) {
	t.Parallel()

	remote := &blockingHasKeyRemote{
		delegate: firebaseidempotencytest.NewInMemoryRemoteStandIn(true),
		started:  make(chan struct{}),
		release:  make(chan struct{}),
	}
	logs := &syncBuffer{}
	dbPath := filepath.Join(t.TempDir(), "shutdown-claim-fence.db")
	cfg := testConfig(t, dbPath, remote)
	cfg.Logger = slog.New(slog.NewJSONHandler(logs, nil))
	cfg.ShutdownDrainTimeout = 5 * time.Second
	a := newApp(t, cfg)
	defer a.Close()
	envelope, err := truths.NewEnvelope(truths.PollOpenedFanout, truths.PollObservedPayload{PollID: "shutdown-claim-fence-poll"})
	if err != nil {
		t.Fatal(err)
	}
	request, err := firebaseidempotency.NewCellRequest("PollOpened", "shutdown-claim-fence-poll", envelope)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.AddCell(request); err != nil {
		t.Fatalf("enqueue poll: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	select {
	case <-remote.started:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("idempotency check did not start")
	}
	cancel()
	waitForDrainStarted(t, logs)
	close(remote.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run app: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("app did not finish its shutdown drain")
	}
	if calls := remote.createKeys.Load(); calls != 0 {
		t.Fatalf("fresh Firestore claims during shutdown = %d, want 0", calls)
	}
	if active := activeWorkCells(t, a.CellarStore()); len(active) != 0 {
		t.Fatalf("gate-refused idempotency Sequence was not terminated: %+v", active)
	}
	store, ok := a.CellarStore().(interface{ DB() *sql.DB })
	if !ok {
		t.Fatal("Cellar store does not expose its shared database")
	}
	var cacheRows int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM firebase_idempotency_records`).Scan(&cacheRows); err != nil {
		t.Fatal(err)
	}
	if cacheRows != 0 {
		t.Fatalf("locally owned idempotency cache rows = %d, want 0", cacheRows)
	}
	if strings.Contains(logs.String(), `"remaining_claim_blocked":1`) {
		t.Fatalf("shutdown reported relinquished work as claim-blocked: %s", logs.String())
	}
	if err := a.Close(); err != nil {
		t.Fatalf("close first app: %v", err)
	}

	restarted := newApp(t, testConfig(t, dbPath, remote))
	defer restarted.Close()
	retryRequest, err := firebaseidempotency.NewCellRequest("PollOpened", "shutdown-claim-fence-poll", envelope)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.AddCell(retryRequest); err != nil {
		t.Fatalf("enqueue poll after retained-database restart: %v", err)
	}
	runUntil(t, restarted, func() bool {
		owner, exists, err := remote.delegate.LookupOwner(context.Background(), "PollOpened", "shutdown-claim-fence-poll")
		return err == nil && exists && owner == string(retryRequest.ID) && owner != string(request.ID)
	})
	if calls := remote.createKeys.Load(); calls != 1 {
		t.Fatalf("remote claim attempts after retained-database retry = %d, want 1", calls)
	}
}

func TestShutdownDrainReconcilesLateClaimResponseByPersistedOwner(t *testing.T) {
	t.Parallel()

	remote := &ambiguousClaimRemote{
		createStarted: make(chan struct{}),
		releaseCreate: make(chan struct{}),
		lookupCalled:  make(chan struct{}),
		lookupExists:  true,
		createErr:     errors.New("claim response lost"),
	}
	a, err, _ := runClaimThroughShutdown(t, remote, 5*time.Second, true)
	defer a.Close()
	if err != nil {
		t.Fatalf("run app: %v", err)
	}
	select {
	case <-remote.lookupCalled:
	default:
		t.Fatal("failed in-flight claim was not reconciled by owner")
	}
	if calls := remote.createKeys.Load(); calls != 1 {
		t.Fatalf("remote claim attempts = %d, want only the original request", calls)
	}
	if active := activeWorkCells(t, a.CellarStore()); len(active) != 0 {
		t.Fatalf("confirmed in-flight claim did not finish its work: %+v", active)
	}
}

func TestShutdownDrainReconciliationOutcomes(t *testing.T) {
	createErr := errors.New("claim response lost")
	lookupErr := errors.New("owner read unavailable")
	for _, test := range []struct {
		name          string
		ownerOverride string
		lookupExists  bool
		lookupErr     error
		wantError     bool
		wantTruth     bool
	}{
		{name: "competing owner", ownerOverride: "other-sequence", lookupExists: true},
		{name: "missing claim", wantError: true},
		{name: "unreadable owner", lookupErr: lookupErr, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			remote := &ambiguousClaimRemote{
				createStarted: make(chan struct{}),
				releaseCreate: make(chan struct{}),
				lookupCalled:  make(chan struct{}),
				createErr:     createErr,
				ownerOverride: test.ownerOverride,
				lookupExists:  test.lookupExists,
				lookupErr:     test.lookupErr,
			}
			a, runErr, logs := runClaimThroughShutdown(t, remote, 5*time.Second, true)
			defer a.Close()
			if (runErr != nil) != test.wantError {
				t.Fatalf("Run() error = %v, wantError %t", runErr, test.wantError)
			}
			if runErr != nil && !errors.Is(runErr, createErr) {
				t.Fatalf("Run() error %v does not wrap the ambiguous create error", runErr)
			}
			if test.lookupErr != nil && !errors.Is(runErr, test.lookupErr) {
				t.Fatalf("Run() error %v does not wrap the owner lookup error", runErr)
			}
			select {
			case <-remote.lookupCalled:
			default:
				t.Fatal("ambiguous claim was not reconciled")
			}
			if calls := remote.createKeys.Load(); calls != 1 {
				t.Fatalf("remote claim attempts = %d, want only the original request", calls)
			}
			if gotTruth := strings.Contains(logs.String(), "idempotency emitting truth"); gotTruth != test.wantTruth {
				t.Fatalf("Truth emitted = %t, want %t", gotTruth, test.wantTruth)
			}
		})
	}
}

func TestShutdownDrainAcceptsLateSuccessfulClaimResponse(t *testing.T) {
	t.Parallel()

	remote := &ambiguousClaimRemote{
		createStarted: make(chan struct{}),
		releaseCreate: make(chan struct{}),
		lookupCalled:  make(chan struct{}),
	}
	a, err, _ := runClaimThroughShutdown(t, remote, 5*time.Second, true)
	defer a.Close()
	if err != nil {
		t.Fatalf("run app: %v", err)
	}
	select {
	case <-remote.lookupCalled:
		t.Fatal("successful claim response performed unnecessary reconciliation")
	default:
	}
	if active := activeWorkCells(t, a.CellarStore()); len(active) != 0 {
		t.Fatalf("confirmed in-flight claim did not finish its work: %+v", active)
	}
}

func TestShutdownDrainDeadlineBoundsClaimReconciliation(t *testing.T) {
	t.Parallel()

	remote := &ambiguousClaimRemote{
		createStarted: make(chan struct{}),
		releaseCreate: make(chan struct{}),
		lookupCalled:  make(chan struct{}),
		lookupErr:     context.DeadlineExceeded,
		waitForCancel: true,
	}
	a, err, logs := runClaimThroughShutdown(t, remote, 100*time.Millisecond, false)
	defer a.Close()
	if err != nil {
		t.Fatalf("run app after drain deadline: %v", err)
	}
	if !strings.Contains(logs.String(), `"outcome":"deadline"`) {
		t.Fatalf("shutdown outcome did not report deadline: %s", logs.String())
	}
	select {
	case <-remote.lookupCalled:
	default:
		t.Fatal("in-flight claim was not accounted for through its cancelled reconciliation")
	}
	if calls := remote.createKeys.Load(); calls != 1 {
		t.Fatalf("remote claim attempts = %d, want only the original request", calls)
	}
}

func TestShutdownDrainReleasesReadyEventVenueClaimForFreshInstance(t *testing.T) {
	t.Parallel()

	recurrenceService, err := recurrencetest.New(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	remote := firebaseidempotencytest.NewInMemoryRemoteStandIn(true)
	logs := &syncBuffer{}
	dbPath := filepath.Join(t.TempDir(), "shutdown-release.db")
	cfg := testConfig(t, dbPath, remote)
	cfg.Logger = slog.New(slog.NewJSONHandler(logs, nil))
	cfg.RecurrenceService = blockingEventPollCreator{
		delegate: recurrenceService,
		started:  make(chan struct{}),
		release:  make(chan struct{}),
	}
	cfg.ShutdownDrainTimeout = 5 * time.Second
	a := newApp(t, cfg)
	defer a.Close()

	observation := truths.EventVenueObserved{
		Venue: recurrence.EventVenue{
			ID:                 "release-event",
			NextOccurrenceDate: "2026-11-14",
		},
		ObservedOn: "2026-11-13",
	}
	fanout, eventKey, owner := seedEventVenueReleaseCandidate(t, a, remote, observation, true)
	if err := addShutdownDrainCell(t, a); err != nil {
		t.Fatalf("add blocking work: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	select {
	case <-cfg.RecurrenceService.(blockingEventPollCreator).started:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("blocking work did not start")
	}
	if err := a.AddCell(fanout); err != nil {
		t.Fatal(err)
	}
	cancel()
	waitForDrainStarted(t, logs)
	close(cfg.RecurrenceService.(blockingEventPollCreator).release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run app: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("app did not finish its shutdown drain")
	}
	if _, exists, err := remote.LookupOwner(context.Background(), "event_venue_observed", eventKey); err != nil || exists {
		t.Fatalf("released remote owner still exists=%t, err=%v", exists, err)
	}
	var cacheRows int
	dbStore, ok := a.CellarStore().(interface{ DB() *sql.DB })
	if !ok {
		t.Fatal("Cellar store does not expose its shared database")
	}
	if err := dbStore.DB().QueryRow(`
		SELECT COUNT(*) FROM firebase_idempotency_records
		WHERE listener = ? AND event_key = ?
	`, "event_venue_observed", eventKey).Scan(&cacheRows); err != nil {
		t.Fatal(err)
	}
	if cacheRows != 0 {
		t.Fatalf("local claim cache rows = %d, want retired row removed", cacheRows)
	}
	active, err := a.CellarStore().ListActive()
	if err != nil {
		t.Fatal(err)
	}
	for _, cell := range active {
		if cell.ID == fanout.ID {
			t.Fatal("released fanout Cell remained in SQLite")
		}
	}

	freshSource := eventvenuestest.New()
	freshSource.Changes = []eventvenues.Change{{Kind: eventvenues.ChangeAdded, Venue: observation.Venue}}
	freshConfig := testConfig(t, filepath.Join(t.TempDir(), "shutdown-release-fresh.db"), remote)
	freshConfig.EventVenueSource = freshSource
	fresh := newApp(t, freshConfig)
	defer fresh.Close()
	runUntil(t, fresh, func() bool {
		_, exists, err := remote.LookupOwner(context.Background(), "event_venue_observed", eventKey)
		return err == nil && exists
	})
	freshOwner, exists, err := remote.LookupOwner(context.Background(), "event_venue_observed", eventKey)
	if err != nil || !exists || freshOwner == owner {
		t.Fatalf("fresh instance claim = %q, %t, %v; want a new owner", freshOwner, exists, err)
	}
}

func TestShutdownDrainDoesNotResumeLocallyRetiredObservationAfterRemoteFailure(t *testing.T) {
	t.Parallel()

	recurrenceService, err := recurrencetest.New(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	delegate := firebaseidempotencytest.NewInMemoryRemoteStandIn(true)
	releaseErr := errors.New("Firestore unavailable during release")
	remote := &failedReleaseRemote{delegate: delegate, err: releaseErr}
	logs := &syncBuffer{}
	dbPath := filepath.Join(t.TempDir(), "shutdown-release-failure.db")
	cfg := testConfig(t, dbPath, remote)
	cfg.Logger = slog.New(slog.NewJSONHandler(logs, nil))
	cfg.RecurrenceService = blockingEventPollCreator{
		delegate: recurrenceService,
		started:  make(chan struct{}),
		release:  make(chan struct{}),
	}
	cfg.ShutdownDrainTimeout = 5 * time.Second
	a := newApp(t, cfg)
	defer a.Close()
	observation := truths.EventVenueObserved{
		Venue:      recurrence.EventVenue{ID: "release-failure-event", NextOccurrenceDate: "2026-11-14"},
		ObservedOn: "2026-11-13",
	}
	fanout, eventKey, owner := seedEventVenueReleaseCandidate(t, a, delegate, observation, true)
	if err := addShutdownDrainCell(t, a); err != nil {
		t.Fatalf("add blocking work: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	blocker := cfg.RecurrenceService.(blockingEventPollCreator)
	select {
	case <-blocker.started:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("blocking work did not start")
	}
	if err := a.AddCell(fanout); err != nil {
		t.Fatal(err)
	}
	cancel()
	waitForDrainStarted(t, logs)
	close(blocker.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run app: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("app did not finish its shutdown drain")
	}
	if !strings.Contains(logs.String(), `"local_retired":true`) || !strings.Contains(logs.String(), `"incomplete":1`) {
		t.Fatalf("incomplete release was not reported honestly: %s", logs.String())
	}
	if remoteOwner, exists, err := delegate.LookupOwner(context.Background(), "event_venue_observed", eventKey); err != nil || !exists || remoteOwner != owner {
		t.Fatalf("remote claim after failed release = %q, %t, %v; want old claim retained", remoteOwner, exists, err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("close first app: %v", err)
	}

	restarted := newApp(t, testConfig(t, dbPath, remote))
	defer restarted.Close()
	active, err := restarted.CellarStore().ListActive()
	if err != nil {
		t.Fatal(err)
	}
	for _, cell := range active {
		if cell.ID == fanout.ID {
			t.Fatal("locally retired observation resumed after retained-database restart")
		}
	}
	dbStore, ok := restarted.CellarStore().(interface{ DB() *sql.DB })
	if !ok {
		t.Fatal("Cellar store does not expose its shared database")
	}
	var cacheRows int
	if err := dbStore.DB().QueryRow(`SELECT COUNT(*) FROM firebase_idempotency_records WHERE listener = ? AND event_key = ?`, "event_venue_observed", eventKey).Scan(&cacheRows); err != nil {
		t.Fatal(err)
	}
	if cacheRows != 0 {
		t.Fatalf("retired cache rows after restart = %d, want 0", cacheRows)
	}
}

func TestShutdownDrainPreservesLegacyOwnerlessObservationClaim(t *testing.T) {
	t.Parallel()

	recurrenceService, err := recurrencetest.New(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	remote := firebaseidempotencytest.NewInMemoryRemoteStandIn(true)
	logs := &syncBuffer{}
	cfg := testConfig(t, filepath.Join(t.TempDir(), "shutdown-ownerless-release.db"), remote)
	cfg.Logger = slog.New(slog.NewJSONHandler(logs, nil))
	cfg.RecurrenceService = blockingEventPollCreator{
		delegate: recurrenceService,
		started:  make(chan struct{}),
		release:  make(chan struct{}),
	}
	cfg.ShutdownDrainTimeout = 5 * time.Second
	a := newApp(t, cfg)
	defer a.Close()
	observation := truths.EventVenueObserved{
		Venue:      recurrence.EventVenue{ID: "ownerless-release-event", NextOccurrenceDate: "2026-11-14"},
		ObservedOn: "2026-11-13",
	}
	fanout, eventKey, owner := seedEventVenueReleaseCandidate(t, a, remote, observation, false)
	if err := addShutdownDrainCell(t, a); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	blocker := cfg.RecurrenceService.(blockingEventPollCreator)
	select {
	case <-blocker.started:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("blocking work did not start")
	}
	if err := a.AddCell(fanout); err != nil {
		t.Fatal(err)
	}
	cancel()
	waitForDrainStarted(t, logs)
	close(blocker.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run app: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("app did not finish its shutdown drain")
	}
	if !strings.Contains(logs.String(), `"stage":"lookup_local_owner"`) {
		t.Fatalf("ownerless cache row was not reported as ineligible: %s", logs.String())
	}
	if remoteOwner, exists, err := remote.LookupOwner(context.Background(), "event_venue_observed", eventKey); err != nil || !exists || remoteOwner != owner {
		t.Fatalf("remote claim = %q, %t, %v; want unchanged claim", remoteOwner, exists, err)
	}
	active, err := a.CellarStore().ListActive()
	if err != nil {
		t.Fatal(err)
	}
	for _, cell := range active {
		if cell.ID == fanout.ID {
			if cell.State != cellar.CellStateReady {
				t.Fatalf("ownerless fanout state = %s, want READY", cell.State)
			}
			return
		}
	}
	t.Fatal("legacy ownerless fanout was retired")
}

func TestShutdownDrainDefersNewPollNotificationToIndependentListener(t *testing.T) {
	t.Parallel()

	recurrenceService, err := recurrencetest.New(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	remote := firebaseidempotencytest.NewInMemoryRemoteStandIn(true)
	newPollSource := newLiveNewPollSource()
	creator := &materializingDrainEventPollCreator{
		delegate: recurrenceService,
		source:   newPollSource,
		started:  make(chan struct{}),
		release:  make(chan struct{}),
	}
	logs := &syncBuffer{}
	dbPath := filepath.Join(t.TempDir(), "shutdown-poll-materialization.db")
	cfg := testConfig(t, dbPath, remote)
	cfg.Logger = slog.New(slog.NewJSONHandler(logs, nil))
	cfg.RecurrenceService = creator
	cfg.NewPollSource = newPollSource
	cfg.ShutdownDrainTimeout = 5 * time.Second
	a := newApp(t, cfg)
	defer a.Close()
	definition, err := cellar.NewCellDefinition(recurrenceplugin.HandlerCreateEventPoll, truths.CreateEventPoll{
		EventID:        "materialized-poll",
		OccurrenceDate: "2026-11-13",
	})
	if err != nil {
		t.Fatal(err)
	}
	request, err := definition.CellRequest()
	if err != nil {
		t.Fatal(err)
	}
	if err := a.AddCell(request); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	select {
	case <-newPollSource.watchStarted:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("new-poll listener did not start")
	}
	select {
	case <-creator.started:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("poll materialization did not start")
	}
	cancel()
	waitForDrainStarted(t, logs)
	select {
	case <-newPollSource.streamStopped:
	case <-time.After(5 * time.Second):
		t.Fatal("new-poll listener did not stop before drain execution")
	}
	close(creator.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run first app: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first app did not finish its shutdown drain")
	}
	if _, exists, err := remote.LookupOwner(context.Background(), "PollOpened", "materialized-poll"); err != nil || exists {
		t.Fatalf("shutdown materialization directly claimed PollOpened = %t, err=%v", exists, err)
	}

	restartedLogs := &syncBuffer{}
	restartedConfig := testConfig(t, dbPath, remote)
	restartedConfig.Logger = slog.New(slog.NewJSONHandler(restartedLogs, nil))
	restartedConfig.NewPollSource = newPollSource
	restarted := newApp(t, restartedConfig)
	defer restarted.Close()
	runUntil(t, restarted, func() bool {
		return strings.Contains(restartedLogs.String(), "idempotency emitting truth")
	})
	if _, exists, err := remote.LookupOwner(context.Background(), "PollOpened", "materialized-poll"); err != nil || !exists {
		t.Fatalf("independent new-poll listener claim = %t, err=%v; want observed poll", exists, err)
	}
}

func seedEventVenueReleaseCandidate(t *testing.T, application *app.App, remote firebaseidempotency.Remote, observation truths.EventVenueObserved, persistOwner bool) (cellar.CellRequest, string, string) {
	t.Helper()
	envelope, err := truths.NewEnvelope(truths.EventVenueObservedFanout, observation)
	if err != nil {
		t.Fatal(err)
	}
	eventKey := observation.Identity()
	sequence, err := firebaseidempotency.NewCellRequest("event_venue_observed", eventKey, envelope)
	if err != nil {
		t.Fatal(err)
	}
	owner := string(sequence.ID)
	if _, _, err := remote.CreateKey(context.Background(), "event_venue_observed", eventKey, owner); err != nil {
		t.Fatal(err)
	}
	seedCellID := cellar.CellID("seed-cache-row-" + observation.Venue.ID)
	if err := application.AddCell(cellar.CellRequest{ID: seedCellID, Steps: []cellar.CellStep{{HandlerName: "test.seed"}}}); err != nil {
		t.Fatal(err)
	}
	var cacheOwner any = owner
	if !persistOwner {
		cacheOwner = nil
	}
	if err := application.CellarStore().RetireReadyCell(seedCellID, func(tx cellar.ApplicationTx) error {
		return tx.Exec(`
			INSERT INTO firebase_idempotency_records(listener, event_key, owner, updated_at)
			VALUES (?, ?, ?, ?)
		`, "event_venue_observed", eventKey, cacheOwner, time.Now().UTC())
	}); err != nil {
		t.Fatalf("seed local claim cache: %v", err)
	}
	fanout, err := envelope.CellRequest()
	if err != nil {
		t.Fatal(err)
	}
	fanout.ID = cellar.CellID("ready-event-venue-fanout-" + observation.Venue.ID)
	return fanout, eventKey, owner
}

func runClaimThroughShutdown(t *testing.T, remote *ambiguousClaimRemote, timeout time.Duration, releaseClaim bool) (*app.App, error, *syncBuffer) {
	t.Helper()
	logs := &syncBuffer{}
	cfg := testConfig(t, filepath.Join(t.TempDir(), "shutdown-claim-reconcile.db"), remote)
	cfg.Logger = slog.New(slog.NewJSONHandler(logs, nil))
	cfg.ShutdownDrainTimeout = timeout
	a := newApp(t, cfg)
	if err := enqueueNewPoll(t, a, "shutdown-claim-reconcile-poll"); err != nil {
		t.Fatalf("enqueue poll: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	select {
	case <-remote.createStarted:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("remote claim did not start")
	}
	cancel()
	waitForDrainStarted(t, logs)
	if releaseClaim {
		close(remote.releaseCreate)
	}
	select {
	case err := <-done:
		return a, err, logs
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("app did not finish its shutdown drain")
		return nil, nil, nil
	}
}

func TestShutdownDrainStopsAndAwaitsListenerCallback(t *testing.T) {
	t.Parallel()

	remote := &countingRemote{delegate: firebaseidempotencytest.NewInMemoryRemoteStandIn(true)}
	logs := &syncBuffer{}
	source := &shutdownEventVenueSource{
		started: make(chan struct{}),
		release: make(chan struct{}),
		stopped: make(chan struct{}),
	}
	cfg := testConfig(t, filepath.Join(t.TempDir(), "shutdown-listener.db"), remote)
	cfg.Logger = slog.New(slog.NewJSONHandler(logs, nil))
	cfg.EventVenueSource = source
	cfg.ShutdownDrainTimeout = 5 * time.Second
	a := newApp(t, cfg)
	defer a.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	select {
	case <-source.started:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("event listener callback did not start")
	}
	cancel()
	waitForDrainStarted(t, logs)
	close(source.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run app: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("app did not await and stop the event listener")
	}
	select {
	case <-source.stopped:
	default:
		t.Fatal("event listener stream was not stopped before Run returned")
	}
	if calls := remote.createKeys.Load(); calls != 0 {
		t.Fatalf("remote claims from shutdown callback = %d, want 0", calls)
	}
	if active := activeWorkCells(t, a.CellarStore()); len(active) != 0 {
		t.Fatalf("gate-refused listener observation was not terminated: %+v", active)
	}
	store, ok := a.CellarStore().(interface{ DB() *sql.DB })
	if !ok {
		t.Fatal("Cellar store does not expose its shared database")
	}
	var cacheRows int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM firebase_idempotency_records`).Scan(&cacheRows); err != nil {
		t.Fatal(err)
	}
	if cacheRows != 0 {
		t.Fatalf("locally owned idempotency cache rows = %d, want 0", cacheRows)
	}
}

func TestShutdownDrainDeadlineCancelsAndPreservesWork(t *testing.T) {
	t.Parallel()

	recurrence, err := recurrencetest.New(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	creator := blockingEventPollCreator{
		delegate: recurrence,
		started:  make(chan struct{}),
		release:  make(chan struct{}),
	}
	cfg := testConfig(t, filepath.Join(t.TempDir(), "shutdown-deadline.db"), firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
	cfg.RecurrenceService = creator
	cfg.ShutdownDrainTimeout = 100 * time.Millisecond
	a := newApp(t, cfg)
	defer a.Close()
	if err := addShutdownDrainCell(t, a); err != nil {
		t.Fatalf("add confirmed cell: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	select {
	case <-creator.started:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("confirmed cell did not start")
	}

	startedShutdown := time.Now()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run app after drain deadline: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("app did not stop after the drain deadline")
	}
	if elapsed := time.Since(startedShutdown); elapsed < cfg.ShutdownDrainTimeout {
		t.Fatalf("shutdown returned after %s, before %s deadline", elapsed, cfg.ShutdownDrainTimeout)
	}
	if active := activeWorkCells(t, a.CellarStore()); len(active) != 1 {
		t.Fatalf("shutdown did not preserve the unfinished cell: %+v", active)
	}
}

func TestZeroShutdownDrainTimeoutRetainsImmediateStop(t *testing.T) {
	t.Parallel()

	recurrence, err := recurrencetest.New(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	creator := blockingEventPollCreator{
		delegate: recurrence,
		started:  make(chan struct{}),
		release:  make(chan struct{}),
	}
	cfg := testConfig(t, filepath.Join(t.TempDir(), "shutdown-zero.db"), firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
	cfg.RecurrenceService = creator
	a := newApp(t, cfg)
	defer a.Close()
	if err := addShutdownDrainCell(t, a); err != nil {
		t.Fatalf("add confirmed cell: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	select {
	case <-creator.started:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("confirmed cell did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run app: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("app did not stop with zero drain timeout")
	}
	if active := activeWorkCells(t, a.CellarStore()); len(active) != 1 {
		t.Fatalf("zero timeout drained unfinished work: %+v", active)
	}
}

func TestShutdownDrainTimeoutRejectsNegativeValue(t *testing.T) {
	cfg := testConfig(t, filepath.Join(t.TempDir(), "negative-timeout.db"), firebaseidempotencytest.NewInMemoryRemoteStandIn(true))
	cfg.ShutdownDrainTimeout = -time.Second
	if _, err := app.New(cfg); err == nil {
		t.Fatal("app.New() accepted a negative shutdown drain timeout")
	}
}

func addShutdownDrainCell(t *testing.T, application *app.App) error {
	t.Helper()
	definition, err := cellar.NewCellDefinition(recurrenceplugin.HandlerCreateEventPoll, truths.CreateEventPoll{
		EventID:        "shutdown-drain-event",
		OccurrenceDate: "2026-11-13",
	})
	if err != nil {
		return err
	}
	request, err := definition.CellRequest()
	if err != nil {
		return err
	}
	return application.AddCell(request)
}

func waitForDrainStarted(t *testing.T, logs *syncBuffer) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if strings.Contains(logs.String(), `"msg":"Landlord shutdown drain started"`) {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("Landlord shutdown drain did not start")
		case <-ticker.C:
		}
	}
}
