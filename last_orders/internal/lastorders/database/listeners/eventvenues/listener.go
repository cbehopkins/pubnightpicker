package eventvenues

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/components/firebaseidempotency"
	"last_orders/internal/lastorders/components/recurrence"
	"last_orders/internal/lastorders/components/venuecache"
	"last_orders/internal/lastorders/database/listeners/lifecycle"
	"last_orders/internal/lastorders/truths"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const listenerEventVenueObserved = "event_venue_observed"

// TimerName is the durable Cellar Timer which drives periodic re-evaluation. See
// docs/cdd/0007-app-structure-migration.md §7.
const TimerName cellar.HandlerName = "eventvenues.reevaluate"

const (
	defaultReevaluateInterval = 24 * time.Hour
	watchRetryDelay           = 5 * time.Second
)

type Config struct {
	Store      cellar.Store
	Service    *recurrence.Service
	Client     *firestore.Client
	VenueCache *venuecache.Service
	// ReevaluateInterval is the initial schedule interval for the durable
	// re-evaluation Timer. Once the Timer has been scheduled, Cellar's persisted
	// configuration is authoritative (see docs/adr/0014); changing this value has
	// no effect on an already-scheduled Timer.
	ReevaluateInterval time.Duration
	Logger             *slog.Logger
}

// Listener observes event venues in the pubs collection and creates Truths for
// venues whose current state requires recurrence or poll-materialisation work.
type Listener struct {
	store    cellar.Store
	service  *recurrence.Service
	client   *firestore.Client
	cache    *venuecache.Service
	interval time.Duration
	logger   *slog.Logger
	lifecycle.Controller
}

// New constructs a new eventvenue Listener.
// This will Construct appropriate Truths either on venue observation or during periodic re-evaluation.
// FIXME is there a timing issue here if we see an update that VenueCache has not yet observed?
func New(cfg Config) (*Listener, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("cellar store is required")
	}
	if cfg.Service == nil {
		return nil, fmt.Errorf("recurrence service is required")
	}
	if cfg.Client == nil {
		return nil, fmt.Errorf("firestore client is required")
	}
	if cfg.ReevaluateInterval <= 0 {
		cfg.ReevaluateInterval = defaultReevaluateInterval
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Listener{store: cfg.Store, service: cfg.Service, client: cfg.Client, cache: cfg.VenueCache, interval: cfg.ReevaluateInterval, logger: cfg.Logger}, nil
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
	iter := l.eventVenueQuery().Snapshots(ctx)
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
			l.createEventVenueObserved(ctx, recurrence.EventVenueFrom(change.Doc))
		}
	}
}

// ReevaluateOnce is the durable Timer callback: it re-runs the eligibility
// predicates so that occurrences crossing a date boundary are observed without a
// document change. A returned error cancels and deletes the Timer (see ADR 0014),
// so failures are logged and swallowed to keep the Timer recurring.
func (l *Listener) ReevaluateOnce(ctx context.Context) error {
	venues, err := l.listEventVenues(ctx)
	if err != nil {
		l.logger.Error("event venue re-evaluation failed", "err", err)
		return nil
	}
	for _, venue := range venues {
		l.createEventVenueObserved(ctx, venue)
	}
	return nil
}

func (l *Listener) eventVenueQuery() firestore.Query {
	return l.client.Collection("pubs").Where("venueType", "==", "event")
}

func (l *Listener) listEventVenues(ctx context.Context) ([]recurrence.EventVenue, error) {
	if l.cache != nil {
		projections, err := l.cache.ListEventVenues(ctx)
		if err != nil {
			return nil, fmt.Errorf("list event venues through cache: %w", err)
		}
		venues := make([]recurrence.EventVenue, 0, len(projections))
		for _, projection := range projections {
			venue, err := eventVenueFromProjection(projection)
			if err != nil {
				return nil, fmt.Errorf("decode venue %q from cache: %w", projection.ID, err)
			}
			venues = append(venues, venue)
		}
		return venues, nil
	}

	iter := l.eventVenueQuery().Documents(ctx)
	defer iter.Stop()

	venues := make([]recurrence.EventVenue, 0, 32)
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			return venues, nil
		}
		if err != nil {
			return nil, err
		}
		venues = append(venues, recurrence.EventVenueFrom(doc))
	}
}

func eventVenueFromProjection(projection venuecache.VenueProjection) (recurrence.EventVenue, error) {
	var recurrenceRule map[string]any
	if projection.RecurrenceJSON != "" {
		if err := json.Unmarshal([]byte(projection.RecurrenceJSON), &recurrenceRule); err != nil {
			return recurrence.EventVenue{}, err
		}
	}
	return recurrence.EventVenue{
		ID:                 projection.ID,
		Name:               projection.Name,
		Recurrence:         recurrenceRule,
		NextOccurrenceDate: projection.NextOccurrenceDate,
	}, nil
}

func (l *Listener) createEventVenueObserved(ctx context.Context, venue recurrence.EventVenue) {
	observedOn := l.service.Today().Format(time.DateOnly)
	event := truths.EventVenueObserved{
		Venue:      venue,
		ObservedOn: observedOn,
	}
	envelope, err := truths.NewEnvelope(truths.EventVenueObservedFanout, event)
	if err != nil {
		l.logger.Error("marshal event venue observation", "event_id", venue.ID, "err", err)
		return
	}
	l.createTruth(ctx, listenerEventVenueObserved, event.Identity(), envelope)
}

// createTruth hands the observation to the idempotency component, which is the sole
// authority on whether the work has already been established.
func (l *Listener) createTruth(ctx context.Context, listener, eventKey string, envelope truths.Envelope) {
	_ = ctx

	request, err := firebaseidempotency.NewCellRequest(listener, eventKey, envelope)
	if err != nil {
		l.logger.Error("build idempotency cell", "listener", listener, "event_key", eventKey, "err", err)
		return
	}

	if _, err := l.store.Add([]cellar.CellRequest{request}); err != nil {
		l.logger.Error("create truth cell", "listener", listener, "event_key", eventKey, "err", err)
		return
	}

	l.logger.Info("truth created", "listener", listener, "event_key", eventKey, "fanout", envelope.FanoutName)
}
