package eventvenues

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/components/firebaseidempotency"
	"last_orders/internal/lastorders/components/recurrence"
	"last_orders/internal/lastorders/handlers"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Idempotency namespaces. The two listeners share a natural key format but are
// deliberately independent identities.
const (
	ListenerStaleEvents = "stale_events"
	ListenerEventDue    = "event_due"
)

const (
	defaultReevaluateInterval = 24 * time.Hour
	watchRetryDelay           = 5 * time.Second
)

type Config struct {
	Store   cellar.Store
	Service *recurrence.Service
	// ReevaluateInterval re-runs the eligibility predicates so that occurrences
	// crossing a date boundary are observed without a document change.
	ReevaluateInterval time.Duration
	Logger             *slog.Logger
}

// Listener observes event venues in the pubs collection and creates Facts for
// venues whose current state requires recurrence or poll-materialisation work.
type Listener struct {
	store    cellar.Store
	service  *recurrence.Service
	interval time.Duration
	logger   *slog.Logger
}

func New(cfg Config) (*Listener, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("cellar store is required")
	}
	if cfg.Service == nil {
		return nil, fmt.Errorf("recurrence service is required")
	}
	if cfg.ReevaluateInterval <= 0 {
		cfg.ReevaluateInterval = defaultReevaluateInterval
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Listener{store: cfg.Store, service: cfg.Service, interval: cfg.ReevaluateInterval, logger: cfg.Logger}, nil
}

func (l *Listener) Start(ctx context.Context) error {
	go l.watch(ctx)
	go l.reevaluate(ctx)
	return nil
}

func (l *Listener) watch(ctx context.Context) {
	for ctx.Err() == nil {
		if err := l.watchOnce(ctx); err != nil {
			l.logger.Error("event venue watch failed", "err", err)
			select {
			case <-ctx.Done():
			case <-time.After(watchRetryDelay):
			}
		}
	}
}

func (l *Listener) watchOnce(ctx context.Context) error {
	iter := l.service.EventVenueQuery().Snapshots(ctx)
	defer iter.Stop()

	for {
		snapshot, err := iter.Next()
		if err != nil {
			if err == iterator.Done || err == context.Canceled || status.Code(err) == codes.Canceled {
				return nil
			}
			return err
		}

		for _, change := range snapshot.Changes {
			if change.Kind == firestore.DocumentRemoved {
				continue
			}
			l.evaluate(ctx, recurrence.EventVenueFrom(change.Doc))
		}
	}
}

func (l *Listener) reevaluate(ctx context.Context) {
	ticker := time.NewTicker(l.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		venues, err := l.service.ListEventVenues(ctx)
		if err != nil {
			l.logger.Error("event venue re-evaluation failed", "err", err)
			continue
		}
		for _, venue := range venues {
			l.evaluate(ctx, venue)
		}
	}
}

func (l *Listener) evaluate(ctx context.Context, venue recurrence.EventVenue) {
	today := l.service.Today()
	loc := l.service.Location()
	key := recurrence.IdempotencyKey(venue.ID, venue.NextOccurrenceDate)

	if recurrence.IsStale(venue.NextOccurrenceDate, today, loc) {
		payload, err := cellar.JSONCodec[handlers.StaleEventPayload]().Marshal(handlers.StaleEventPayload{
			EventID:      venue.ID,
			ObservedDate: venue.NextOccurrenceDate,
		})
		if err != nil {
			l.logger.Error("marshal stale event payload", "event_id", venue.ID, "err", err)
			return
		}
		l.createFact(ctx, ListenerStaleEvents, key, handlers.HandlerStaleEvent, payload)
		return
	}

	if recurrence.IsDue(venue.NextOccurrenceDate, today, loc) {
		payload, err := cellar.JSONCodec[handlers.CreateEventPollPayload]().Marshal(handlers.CreateEventPollPayload{
			EventID:        venue.ID,
			OccurrenceDate: venue.NextOccurrenceDate,
		})
		if err != nil {
			l.logger.Error("marshal create event poll payload", "event_id", venue.ID, "err", err)
			return
		}
		l.createFact(ctx, ListenerEventDue, key, handlers.HandlerCreateEventPoll, payload)
	}
}

// createFact hands the observation to the idempotency component, which is the sole
// authority on whether the work has already been established.
func (l *Listener) createFact(ctx context.Context, listener, eventKey string, target cellar.HandlerName, payload []byte) {
	_ = ctx

	raw, err := cellar.JSONCodec[firebaseidempotency.PendingPayload]().Marshal(firebaseidempotency.PendingPayload{
		Listener: listener,
		EventKey: eventKey,
		Fanout: []firebaseidempotency.FanoutTarget{{
			HandlerName: target,
			Payload:     payload,
		}},
	})
	if err != nil {
		l.logger.Error("marshal pending payload", "listener", listener, "event_key", eventKey, "err", err)
		return
	}

	if _, err := l.store.Add([]cellar.CellRequest{{
		HandlerName: firebaseidempotency.HandlerPending,
		Payload:     raw,
	}}); err != nil {
		l.logger.Error("create fact cell", "listener", listener, "event_key", eventKey, "err", err)
		return
	}

	l.logger.Info("fact created", "listener", listener, "event_key", eventKey, "handler", target)
}
