package recurrenceplugin

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/truths"
)

type EventVenueClaimReleaseStore interface {
	ListActive() ([]cellar.Cell, error)
	RetireReadyCell(cellar.CellID, ...cellar.ApplicationWork) error
}

type EventVenueClaimReleaseCache interface {
	LookupOwner(context.Context, string, string) (string, bool, error)
	RetireCacheEntryWork(string, string, string) cellar.ApplicationWork
}

type EventVenueClaimReleaseRemote interface {
	ReleaseKey(context.Context, string, string, string) (bool, error)
}

type EventVenueClaimReleaseConfig struct {
	Store    EventVenueClaimReleaseStore
	Cache    EventVenueClaimReleaseCache
	Remote   EventVenueClaimReleaseRemote
	Listener string
	Logger   *slog.Logger
}

type EventVenueClaimReleaser struct {
	store    EventVenueClaimReleaseStore
	cache    EventVenueClaimReleaseCache
	remote   EventVenueClaimReleaseRemote
	listener string
	logger   *slog.Logger
}

func NewEventVenueClaimReleaser(cfg EventVenueClaimReleaseConfig) (*EventVenueClaimReleaser, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("event venue claim release store is required")
	}
	if cfg.Cache == nil {
		return nil, fmt.Errorf("event venue claim release cache is required")
	}
	if cfg.Remote == nil {
		return nil, fmt.Errorf("event venue claim release remote is required")
	}
	if cfg.Listener == "" {
		return nil, fmt.Errorf("event venue claim release listener is required")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &EventVenueClaimReleaser{
		store: cfg.Store, cache: cfg.Cache, remote: cfg.Remote,
		listener: cfg.Listener, logger: cfg.Logger,
	}, nil
}

func (r *EventVenueClaimReleaser) ReleaseUnfulfilled(ctx context.Context) {
	cells, err := r.store.ListActive()
	if err != nil {
		r.logger.Warn("event venue claim release incomplete", "stage", "list_candidates", "err", err)
		return
	}
	var candidates, released, incomplete int
	for _, cell := range cells {
		observation, ok := readyEventVenueObservation(cell)
		if !ok {
			continue
		}
		candidates++
		if err := ctx.Err(); err != nil {
			incomplete++
			r.logger.Warn("event venue claim release skipped", "cell_id", cell.ID, "stage", "deadline", "err", err)
			continue
		}
		eventKey := observation.Identity()
		owner, exists, err := r.cache.LookupOwner(ctx, r.listener, eventKey)
		if err != nil || !exists || owner == "" {
			incomplete++
			r.logger.Warn("event venue claim release incomplete", "cell_id", cell.ID, "event_key", eventKey, "stage", "lookup_local_owner", "owner_exists", exists, "err", err)
			continue
		}
		if err := r.store.RetireReadyCell(
			cell.ID,
			r.cache.RetireCacheEntryWork(r.listener, eventKey, owner),
		); err != nil {
			incomplete++
			r.logger.Warn("event venue claim release incomplete", "cell_id", cell.ID, "event_key", eventKey, "stage", "retire_local_work", "err", err)
			continue
		}
		confirmed, err := r.remote.ReleaseKey(ctx, r.listener, eventKey, owner)
		if err != nil || !confirmed {
			incomplete++
			r.logger.Warn("event venue claim release incomplete", "cell_id", cell.ID, "event_key", eventKey, "owner", owner, "stage", "release_remote_claim", "local_retired", true, "confirmed", confirmed, "err", err)
			continue
		}
		released++
		r.logger.Info("event venue observation claim released", "cell_id", cell.ID, "event_key", eventKey, "owner", owner)
	}
	if candidates > 0 {
		r.logger.Info("event venue claim release finished", "candidates", candidates, "released", released, "incomplete", incomplete)
	}
}

func readyEventVenueObservation(cell cellar.Cell) (truths.EventVenueObserved, bool) {
	if cell.State != cellar.CellStateReady || cell.CurrentStep != 0 || len(cell.Steps) != 1 || cell.Steps[0].HandlerName != truths.EventVenueObservedFanout {
		return truths.EventVenueObserved{}, false
	}
	var observation truths.EventVenueObserved
	if err := json.Unmarshal(cell.Steps[0].Payload, &observation); err != nil || observation.Venue.ID == "" || observation.ObservedOn == "" {
		return truths.EventVenueObserved{}, false
	}
	return observation, true
}
