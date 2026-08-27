// Package idempotency implements the local-only idempotency variant described in
// docs/cdd/0001-idempotency.md §6. It is used where the local Base DB is itself the
// authority (timers, housekeeping, backend-internal observations) — no external
// remote store is consulted.
package idempotency

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/basestore"
	"last_orders/internal/lastorders/components/facts"
)

// HandlerCheck is the single-step Cell which checks and establishes a local identity.
const HandlerCheck cellar.HandlerName = "idempotency.check"

// Store is the local durable record of claimed idempotency identities.
type Store struct {
	base *basestore.Store
}

func New(base *basestore.Store) (*Store, error) {
	if base == nil || base.DB() == nil {
		return nil, fmt.Errorf("base store is required")
	}
	if _, err := base.DB().Exec(`
		CREATE TABLE IF NOT EXISTS idempotency_records (
			component TEXT NOT NULL,
			key TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY(component, key)
		);
	`); err != nil {
		return nil, fmt.Errorf("create idempotency_records schema: %w", err)
	}
	return &Store{base: base}, nil
}

func (s *Store) Exists(ctx context.Context, component, key string) (bool, error) {
	var found int
	err := s.base.DB().QueryRowContext(ctx, `
		SELECT 1 FROM idempotency_records WHERE component = ? AND key = ?
	`, component, key).Scan(&found)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// InsertUnlessExistsWork claims the identity. It rejects the transition if a
// concurrent claim already won, so the loser retries and observes it as claimed.
func (s *Store) InsertUnlessExistsWork(component, key string) cellar.ApplicationWork {
	return func(tx cellar.ApplicationTx) error {
		if err := tx.Exec(`
			INSERT INTO idempotency_records(component, key, created_at)
			VALUES(?, ?, ?)
			ON CONFLICT(component, key) DO NOTHING
		`, component, key, time.Now().UTC()); err != nil {
			return err
		}
		var changed int64
		if err := tx.QueryRow(`SELECT changes()`).Scan(&changed); err != nil {
			return err
		}
		if changed != 1 {
			return ErrClaimRejected
		}
		return nil
	}
}

var ErrClaimRejected = fmt.Errorf("idempotency claim rejected")

// CheckPayload identifies the observation and the Fact to emit once established.
type CheckPayload struct {
	Component string     `json:"component"`
	Key       string     `json:"key"`
	Fact      facts.Fact `json:"fact"`
}

// NewCellRequest builds the single-step Cell which performs the check-and-establish.
func NewCellRequest(component, key string, fact facts.Fact) (cellar.CellRequest, error) {
	payload, err := cellar.JSONCodec[CheckPayload]().Marshal(CheckPayload{Component: component, Key: key, Fact: fact})
	if err != nil {
		return cellar.CellRequest{}, err
	}
	return cellar.CellRequest{Steps: []cellar.CellStep{{HandlerName: HandlerCheck, Payload: payload}}}, nil
}

// CheckHandler is Step 1 (and only step): duplicate -> Kill, new -> claim and emit.
type CheckHandler struct {
	Store  *Store
	Logger *slog.Logger
}

func (h CheckHandler) Handle(ctx context.Context, payload CheckPayload) cellar.Result {
	exists, err := h.Store.Exists(ctx, payload.Component, payload.Key)
	if err != nil {
		return cellar.ErrorResult{Message: "load idempotency state", Err: err}
	}
	if exists {
		return cellar.Kill{}
	}

	factCell, err := facts.CellRequest(payload.Fact.Name, payload.Fact.Payload)
	if err != nil {
		return cellar.ErrorResult{Message: "build fact cell", Err: err}
	}
	if h.Logger != nil {
		h.Logger.Info("idempotency claiming and emitting fact", "component", payload.Component, "key", payload.Key, "fact", payload.Fact.Name)
	}
	return cellar.Complete{
		NewCells:        []cellar.CellRequest{factCell},
		ApplicationWork: []cellar.ApplicationWork{h.Store.InsertUnlessExistsWork(payload.Component, payload.Key)},
	}
}
