package counter

import (
	"context"
	"fmt"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/basestore"
)

const DefaultCounter = "vertical_slice_counter"

type Store struct {
	base *basestore.Store
}

func New(base *basestore.Store) (*Store, error) {
	if base == nil || base.DB() == nil {
		return nil, fmt.Errorf("base store is required")
	}

	db := base.DB()
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS app_counters (
			name TEXT PRIMARY KEY,
			value INTEGER NOT NULL
		);
	`); err != nil {
		return nil, fmt.Errorf("create app_counters schema: %w", err)
	}
	if _, err := db.Exec(`
		INSERT INTO app_counters(name, value)
		VALUES(?, 0)
		ON CONFLICT(name) DO NOTHING;
	`, DefaultCounter); err != nil {
		return nil, fmt.Errorf("seed app_counters default row: %w", err)
	}

	return &Store{base: base}, nil
}

func (s *Store) IncrementWork(counterName string, delta int64) cellar.ApplicationWork {
	if counterName == "" {
		counterName = DefaultCounter
	}
	return func(tx cellar.ApplicationTx) error {
		return tx.Exec(`UPDATE app_counters SET value = value + ? WHERE name = ?`, delta, counterName)
	}
}

func (s *Store) Value(ctx context.Context, counterName string) (int64, error) {
	if counterName == "" {
		counterName = DefaultCounter
	}
	var value int64
	err := s.base.DB().QueryRowContext(ctx, `SELECT value FROM app_counters WHERE name = ?`, counterName).Scan(&value)
	if err != nil {
		return 0, err
	}
	return value, nil
}
