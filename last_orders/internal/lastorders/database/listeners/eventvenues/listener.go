package eventvenues

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/components/facts"
	"last_orders/internal/lastorders/components/firebaseidempotency"
	"last_orders/internal/lastorders/components/recurrence"
	"last_orders/internal/lastorders/database/listeners/lifecycle"
	recurrenceplugin "last_orders/internal/lastorders/plugins/recurrence"

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

// TimerName is the durable Cellar Timer which drives periodic re-evaluation. See
// docs/cdd/0007-app-structure-migration.md §7.
const TimerName cellar.HandlerName = "eventvenues.reevaluate"

const (
	defaultReevaluateInterval = 24 * time.Hour
	watchRetryDelay           = 5 * time.Second
)

type Config struct {
	Store   cellar.Store
	Service *recurrence.Service
	// ReevaluateInterval is the initial schedule interval for the durable
	// re-evaluation Timer. Once the Timer has been scheduled, Cellar's persisted
	// configuration is authoritative (see docs/adr/0014); changing this value has
	// no effect on an already-scheduled Timer.
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
	lifecycle.Controller
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

// Interval returns the initial schedule interval for the durable re-evaluation Timer.
func (l *Listener) Interval() time.Duration {
	return l.interval
}

func (l *Listener) Start(ctx context.Context) error {
	return l.Controller.Start(ctx, l.watch)
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

// ReevaluateOnce is the durable Timer callback: it re-runs the eligibility
// predicates so that occurrences crossing a date boundary are observed without a
// document change. A returned error cancels and deletes the Timer (see ADR 0014),
// so failures are logged and swallowed to keep the Timer recurring.
func (l *Listener) ReevaluateOnce(ctx context.Context) error {
	venues, err := l.service.ListEventVenues(ctx)
	if err != nil {
		l.logger.Error("event venue re-evaluation failed", "err", err)
		return nil
	}
	for _, venue := range venues {
		l.evaluate(ctx, venue)
	}
	return nil
}

func (l *Listener) evaluate(ctx context.Context, venue recurrence.EventVenue) {
	today := l.service.Today()
	loc := l.service.Location()

	if recurrence.NeedsRecalculation(venue.Recurrence, venue.NextOccurrenceDate, today, loc) {
		payload, err := cellar.JSONCodec[recurrenceplugin.StaleEventPayload]().Marshal(recurrenceplugin.StaleEventPayload{
			EventID:      venue.ID,
			ObservedDate: venue.NextOccurrenceDate,
		})
		if err != nil {
			l.logger.Error("marshal stale event payload", "event_id", venue.ID, "err", err)
			return
		}
		key := recurrence.StaleEventKey(venue.ID, venue.NextOccurrenceDate, venue.Recurrence)
		l.createFact(ctx, ListenerStaleEvents, key, recurrenceplugin.FactStaleEvent, payload)
		return
	}

	if recurrence.IsDue(venue.NextOccurrenceDate, today, loc) {
		payload, err := cellar.JSONCodec[recurrenceplugin.CreateEventPollPayload]().Marshal(recurrenceplugin.CreateEventPollPayload{
			EventID:        venue.ID,
			OccurrenceDate: venue.NextOccurrenceDate,
		})
		if err != nil {
			l.logger.Error("marshal create event poll payload", "event_id", venue.ID, "err", err)
			return
		}
		key := recurrence.EventDueKey(venue.ID, venue.NextOccurrenceDate)
		l.createFact(ctx, ListenerEventDue, key, recurrenceplugin.FactCreateEventPoll, payload)
	}
}

// createFact hands the observation to the idempotency component, which is the sole
// authority on whether the work has already been established.
func (l *Listener) createFact(ctx context.Context, listener, eventKey, factName string, payload []byte) {
	_ = ctx

	request, err := firebaseidempotency.NewCellRequest(listener, eventKey, facts.Fact{Name: factName, Payload: payload})
	if err != nil {
		l.logger.Error("build idempotency cell", "listener", listener, "event_key", eventKey, "err", err)
		return
	}

	if _, err := l.store.Add([]cellar.CellRequest{request}); err != nil {
		l.logger.Error("create fact cell", "listener", listener, "event_key", eventKey, "err", err)
		return
	}

	l.logger.Info("fact created", "listener", listener, "event_key", eventKey, "fact", factName)
}
