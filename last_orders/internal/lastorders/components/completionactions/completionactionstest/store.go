// Package completionactionstest provides an in-memory completionactions.Store for tests.
package completionactionstest

import (
	"context"
	"slices"
	"sync"

	"last_orders/internal/lastorders/components/completionactions"
)

type Store struct {
	mu      sync.Mutex
	records map[completionactions.Collection]map[string]completionactions.Record
}

func New() *Store {
	return &Store{records: map[completionactions.Collection]map[string]completionactions.Record{}}
}

func (s *Store) Get(_ context.Context, collection completionactions.Collection, pollID string) (completionactions.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record := completionactions.Record{}
	for action, keys := range s.records[collection][pollID] {
		record[action] = slices.Clone(keys)
	}
	return record, nil
}

func (s *Store) Mark(_ context.Context, collection completionactions.Collection, pollID string, action completionactions.ActionType, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.records[collection] == nil {
		s.records[collection] = map[string]completionactions.Record{}
	}
	record := s.records[collection][pollID]
	if record == nil {
		record = completionactions.Record{}
		s.records[collection][pollID] = record
	}
	if !slices.Contains(record[action], key) {
		record[action] = append(record[action], key)
	}
	return nil
}
