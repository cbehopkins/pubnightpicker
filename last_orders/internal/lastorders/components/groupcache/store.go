package groupcache

import (
	"context"
	"fmt"

	"last_orders/internal/lastorders/basestore"
)

type Store struct {
	base *basestore.Store
}

type Mutation struct {
	ID    GroupID
	Group *Group
}

func NewStore(base *basestore.Store) (*Store, error) {
	if base == nil || base.DB() == nil {
		return nil, fmt.Errorf("base store is required")
	}
	if _, err := base.DB().ExecContext(context.Background(), `
		CREATE TABLE IF NOT EXISTS group_cache (
			group_id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			visibility TEXT NOT NULL CHECK(visibility IN ('public', 'private'))
		);
		CREATE UNIQUE INDEX IF NOT EXISTS group_cache_exact_name ON group_cache(name COLLATE BINARY);
	`); err != nil {
		return nil, fmt.Errorf("create Group projection: %w", err)
	}
	return &Store{base: base}, nil
}

func (s *Store) ListPublic(ctx context.Context, exactName *string) ([]Group, error) {
	query := `SELECT group_id, name, visibility FROM group_cache WHERE visibility = ?`
	args := []any{VisibilityPublic}
	if exactName != nil {
		query += ` AND name = ? COLLATE BINARY`
		args = append(args, *exactName)
	}
	query += ` ORDER BY name COLLATE BINARY, group_id COLLATE BINARY`
	rows, err := s.base.DB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := make([]Group, 0)
	for rows.Next() {
		var group Group
		if err := rows.Scan(&group.ID, &group.Name, &group.Visibility); err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return groups, nil
}

func (s *Store) ApplyBatch(ctx context.Context, mutations []Mutation, baseline bool) error {
	seenIDs := make(map[GroupID]struct{}, len(mutations))
	seenNames := make(map[string]struct{}, len(mutations))
	for _, mutation := range mutations {
		if mutation.ID == "" {
			return fmt.Errorf("group ID is required")
		}
		if baseline {
			if _, exists := seenIDs[mutation.ID]; exists {
				return fmt.Errorf("duplicate Group ID %q in baseline", mutation.ID)
			}
			seenIDs[mutation.ID] = struct{}{}
		}
		if mutation.Group == nil {
			continue
		}
		if mutation.Group.ID != mutation.ID {
			return fmt.Errorf("Group mutation ID %q does not match Group ID %q", mutation.ID, mutation.Group.ID)
		}
		if err := validateGroup(*mutation.Group); err != nil {
			return err
		}
		if baseline {
			if _, exists := seenNames[mutation.Group.Name]; exists {
				return fmt.Errorf("duplicate exact Group name %q in baseline", mutation.Group.Name)
			}
			seenNames[mutation.Group.Name] = struct{}{}
		}
	}

	tx, err := s.base.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if baseline {
		if _, err := tx.ExecContext(ctx, `DELETE FROM group_cache`); err != nil {
			return fmt.Errorf("replace Group projection baseline: %w", err)
		}
	}
	for _, mutation := range mutations {
		if mutation.Group == nil {
			if _, err := tx.ExecContext(ctx, `DELETE FROM group_cache WHERE group_id = ?`, mutation.ID); err != nil {
				return fmt.Errorf("remove Group %q from projection: %w", mutation.ID, err)
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO group_cache(group_id, name, visibility) VALUES(?, ?, ?)
			ON CONFLICT(group_id) DO UPDATE SET name = excluded.name, visibility = excluded.visibility
		`, mutation.Group.ID, mutation.Group.Name, mutation.Group.Visibility); err != nil {
			return fmt.Errorf("persist Group %q projection: %w", mutation.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit Group projection: %w", err)
	}
	return nil
}

func validateGroup(group Group) error {
	if group.ID == "" || group.Name == "" {
		return fmt.Errorf("Group ID and name are required")
	}
	if group.Visibility != VisibilityPublic && group.Visibility != VisibilityPrivate {
		return fmt.Errorf("Group %q has unsupported visibility %q", group.ID, group.Visibility)
	}
	return nil
}
