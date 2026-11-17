package groupcache

import (
	"context"
	"fmt"
)

type Service struct {
	store  *Store
	source Source
}

func NewService(store *Store, source Source) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("Group projection store is required")
	}
	if source == nil {
		return nil, fmt.Errorf("Group source is required")
	}
	return &Service{store: store, source: source}, nil
}

func (s *Service) ListPublic(ctx context.Context, exactName *string) ([]Group, error) {
	return s.store.ListPublic(ctx, exactName)
}

func (s *Service) Watch(ctx context.Context) (ChangeStream, error) {
	return s.source.Watch(ctx)
}
