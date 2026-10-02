package durableemail

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// ProgressFilter narrows a progress query. The zero value selects every row,
// and populated fields combine conjunctively.
type ProgressFilter struct {
	IdempotencyToken string
	Recipient        string
	States           []string
	Limit            int
	Offset           int
}

// ProgressRow reports one recipient of one logical Send operation.
type ProgressRow struct {
	IdempotencyToken string
	Recipient        string
	RecipientName    string
	State            string
	PMUID            string
	SubmittedAt      *time.Time
	Subject          string
	SenderEmail      string
	SenderName       string
	TemplateID       string
}

// QueryProgress reports recipient progress for display. It is a reporting view
// and takes no part in the Send sequence.
func (s *Store) QueryProgress(ctx context.Context, filter ProgressFilter) ([]ProgressRow, error) {
	var conditions []string
	var args []any

	if filter.IdempotencyToken != "" {
		conditions = append(conditions, "p.idempotency_token = ?")
		args = append(args, filter.IdempotencyToken)
	}
	if filter.Recipient != "" {
		conditions = append(conditions, "p.recipient = ?")
		args = append(args, filter.Recipient)
	}
	if len(filter.States) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(filter.States)), ", ")
		conditions = append(conditions, "p.state IN ("+placeholders+")")
		for _, state := range filter.States {
			args = append(args, state)
		}
	}

	query := `
		SELECT
			p.idempotency_token,
			p.recipient,
			p.recipient_name,
			p.state,
			p.pmuid,
			p.submitted_at,
			r.subject,
			r.sender_email,
			r.sender_name,
			r.template_id
		FROM email_progress p
		JOIN email_requests r ON r.idempotency_token = p.idempotency_token
	`
	if len(conditions) > 0 {
		query += "WHERE " + strings.Join(conditions, " AND ") + "\n"
	}
	// Primary key order, so pages of an unchanged table are disjoint.
	query += "ORDER BY p.idempotency_token, p.recipient\n"

	if filter.Limit > 0 {
		query += "LIMIT ?\n"
		args = append(args, filter.Limit)
	} else if filter.Offset > 0 {
		// SQLite requires a LIMIT before OFFSET; -1 means unbounded.
		query += "LIMIT -1\n"
	}
	if filter.Offset > 0 {
		query += "OFFSET ?\n"
		args = append(args, filter.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query email progress: %w", err)
	}
	defer rows.Close()

	results := []ProgressRow{}
	for rows.Next() {
		var row ProgressRow
		var pmuid sql.NullString
		var submittedAt sql.NullTime
		if err := rows.Scan(
			&row.IdempotencyToken,
			&row.Recipient,
			&row.RecipientName,
			&row.State,
			&pmuid,
			&submittedAt,
			&row.Subject,
			&row.SenderEmail,
			&row.SenderName,
			&row.TemplateID,
		); err != nil {
			return nil, fmt.Errorf("scan email progress: %w", err)
		}
		row.PMUID = pmuid.String
		if submittedAt.Valid {
			value := submittedAt.Time.UTC()
			row.SubmittedAt = &value
		}
		results = append(results, row)
	}

	return results, rows.Err()
}

// EventRow reports one recorded provider event for a recipient.
type EventRow struct {
	IdempotencyToken string
	Recipient        string
	Event            DeliveryEvent
	RecordedAt       time.Time
}

// QueryEvents reports webhook history in receipt order. State filters apply to
// current progress, not to historical events.
func (s *Store) QueryEvents(ctx context.Context, filter ProgressFilter) ([]EventRow, error) {
	if len(filter.States) != 0 {
		return nil, fmt.Errorf("event history cannot be filtered by progress state")
	}
	var conditions []string
	var args []any
	if filter.IdempotencyToken != "" {
		conditions = append(conditions, "idempotency_token = ?")
		args = append(args, filter.IdempotencyToken)
	}
	if filter.Recipient != "" {
		conditions = append(conditions, "recipient = ?")
		args = append(args, filter.Recipient)
	}
	query := `SELECT idempotency_token, recipient, event, recorded_at FROM email_events` + "\n"
	if len(conditions) > 0 {
		query += "WHERE " + strings.Join(conditions, " AND ") + "\n"
	}
	query += "ORDER BY id\n"
	if filter.Limit > 0 {
		query += "LIMIT ?\n"
		args = append(args, filter.Limit)
	} else if filter.Offset > 0 {
		query += "LIMIT -1\n"
	}
	if filter.Offset > 0 {
		query += "OFFSET ?\n"
		args = append(args, filter.Offset)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query email events: %w", err)
	}
	defer rows.Close()
	results := []EventRow{}
	for rows.Next() {
		var row EventRow
		if err := rows.Scan(&row.IdempotencyToken, &row.Recipient, &row.Event, &row.RecordedAt); err != nil {
			return nil, fmt.Errorf("scan email event: %w", err)
		}
		row.RecordedAt = row.RecordedAt.UTC()
		results = append(results, row)
	}
	return results, rows.Err()
}
