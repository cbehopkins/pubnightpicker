package app_test

import (
	"context"
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
	recurrenceplugin "last_orders/internal/lastorders/plugins/recurrence"
	"last_orders/internal/lastorders/truths"
)

type blockingEventPollCreator struct {
	delegate *recurrencetest.Service
	started  chan struct{}
	release  chan struct{}
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

func (r *countingRemote) HasKey(ctx context.Context, listener, eventKey string) (bool, error) {
	return r.delegate.HasKey(ctx, listener, eventKey)
}

func (r *countingRemote) CreateKey(ctx context.Context, listener, eventKey, owner string) (bool, bool, error) {
	r.createKeys.Add(1)
	return r.delegate.CreateKey(ctx, listener, eventKey, owner)
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

func (r *blockingHasKeyRemote) HasKey(ctx context.Context, listener, eventKey string) (bool, error) {
	r.startOnce.Do(func() { close(r.started) })
	select {
	case <-r.release:
	case <-ctx.Done():
		return false, ctx.Err()
	}
	return r.delegate.HasKey(ctx, listener, eventKey)
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

func TestShutdownDrainBlocksFreshClaimAndPreservesSequence(t *testing.T) {
	t.Parallel()

	remote := &blockingHasKeyRemote{
		delegate: firebaseidempotencytest.NewInMemoryRemoteStandIn(true),
		started:  make(chan struct{}),
		release:  make(chan struct{}),
	}
	logs := &syncBuffer{}
	cfg := testConfig(t, filepath.Join(t.TempDir(), "shutdown-claim-fence.db"), remote)
	cfg.Logger = slog.New(slog.NewJSONHandler(logs, nil))
	cfg.ShutdownDrainTimeout = 5 * time.Second
	a := newApp(t, cfg)
	defer a.Close()
	if err := enqueueNewPoll(t, a, "shutdown-claim-fence-poll"); err != nil {
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
	active := activeWorkCells(t, a.CellarStore())
	if len(active) != 1 || active[0].CurrentStep != 1 || active[0].State != cellar.CellStateReady || active[0].Steps[1].HandlerName != firebaseidempotency.HandlerPopulateRemote {
		t.Fatalf("blocked idempotency Sequence was not preserved at its claim step: %+v", active)
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
	if active := activeWorkCells(t, a.CellarStore()); len(active) != 1 || active[0].CurrentStep != 0 {
		t.Fatalf("late listener observation was not preserved as unclaimed work: %+v", active)
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
