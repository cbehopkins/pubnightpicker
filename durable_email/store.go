package durableemail

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"cellar/pkg/cellar"
)

// Progress states of a recipient row.
const (
	StatePending         = "Pending"
	StateAccepted        = "Accepted"
	StateRecovery        = "Recovery"
	StateRecoveryWaiting = "RecoveryWaiting"
)

// Store owns durable email data stored in a caller-owned database.
type Store struct {
	db *sql.DB
}

// NewStore creates the durable email tables if they do not already exist.
func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("db is required")
	}

	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin durable email schema transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS email_requests (
			idempotency_token TEXT NOT NULL PRIMARY KEY,
			message_id TEXT NOT NULL UNIQUE,
			sender_email TEXT NOT NULL,
			sender_name TEXT NOT NULL,
			subject TEXT NOT NULL,
			template_id TEXT NOT NULL,
			text TEXT NOT NULL,
			variables TEXT NOT NULL,
			headers TEXT NOT NULL
		);
	`); err != nil {
		return nil, fmt.Errorf("create email_requests schema: %w", err)
	}

	if _, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS email_progress (
			idempotency_token TEXT NOT NULL,
			recipient TEXT NOT NULL,
			recipient_name TEXT NOT NULL,
			state TEXT NOT NULL,
			pmuid TEXT,
			variables TEXT NOT NULL,
			submitted_at DATETIME,
			PRIMARY KEY (idempotency_token, recipient)
		);
	`); err != nil {
		return nil, fmt.Errorf("create email_progress schema: %w", err)
	}

	// The progress primary key cannot serve queries that do not lead with the token.
	if _, err := tx.Exec(`
		CREATE INDEX IF NOT EXISTS email_progress_recipient
			ON email_progress (recipient);
	`); err != nil {
		return nil, fmt.Errorf("create email_progress recipient index: %w", err)
	}

	if _, err := tx.Exec(`
		CREATE INDEX IF NOT EXISTS email_progress_state
			ON email_progress (state);
	`); err != nil {
		return nil, fmt.Errorf("create email_progress state index: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit durable email schema: %w", err)
	}

	return &Store{db: db}, nil
}

// RecoverSubmissions marks interrupted submissions before Cellar starts.
func (s *Store) RecoverSubmissions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE email_progress SET state = ?
		WHERE state = ? AND submitted_at IS NOT NULL
	`, StateRecovery, StatePending)
	return err
}

// durableRequest is the operation-wide data needed to rebuild a provider request.
type durableRequest struct {
	messageID   string
	senderEmail string
	senderName  string
	subject     string
	templateID  string
	text        string
	variables   map[string]any
	headers     map[string]string
}

// durableRecipient is the recipient-specific data needed to rebuild a provider request.
type durableRecipient struct {
	email     string
	name      string
	variables map[string]any
}

// insertRequestWork stores the request and its recipient rows in one transaction.
func (s *Store) insertRequestWork(request SendRequest) (cellar.ApplicationWork, error) {
	messageID, err := newMessageID()
	if err != nil {
		return nil, err
	}
	commonVariables, err := marshalObject(request.Variables)
	if err != nil {
		return nil, fmt.Errorf("encode request variables: %w", err)
	}
	headers, err := marshalObject(request.Headers)
	if err != nil {
		return nil, fmt.Errorf("encode request headers: %w", err)
	}

	type recipientRow struct {
		email     string
		name      string
		variables string
	}
	rows := make([]recipientRow, 0, len(request.Recipients))
	wanted := make(map[string]recipientRow, len(request.Recipients))
	for _, recipient := range request.Recipients {
		variables, err := marshalObject(recipient.Variables)
		if err != nil {
			return nil, fmt.Errorf("encode variables for %q: %w", recipient.Email, err)
		}
		if _, exists := wanted[recipient.Email]; exists {
			return nil, fmt.Errorf("duplicate recipient %q", recipient.Email)
		}
		row := recipientRow{email: recipient.Email, name: recipient.Name, variables: variables}
		rows = append(rows, row)
		wanted[recipient.Email] = row
	}

	return func(tx cellar.ApplicationTx) error {
		var senderEmail, senderName, subject, templateID, text, storedVariables, storedHeaders string
		err := tx.QueryRow(`
			SELECT sender_email, sender_name, subject, template_id, text, variables, headers
			FROM email_requests WHERE idempotency_token = ?
		`, request.IdempotencyToken).Scan(&senderEmail, &senderName, &subject, &templateID, &text, &storedVariables, &storedHeaders)
		if err == nil {
			if senderEmail != request.SenderEmail || senderName != request.SenderName || subject != request.Subject ||
				templateID != request.TemplateID || text != request.Text ||
				!sameJSON(storedVariables, commonVariables) || !sameJSON(storedHeaders, headers) {
				return fmt.Errorf("conflicting request for idempotency token %q", request.IdempotencyToken)
			}
			storedRows, err := tx.Query(`
				SELECT recipient, recipient_name, variables FROM email_progress
				WHERE idempotency_token = ?
			`, request.IdempotencyToken)
			if err != nil {
				return err
			}
			seen := 0
			for storedRows.Next() {
				var email, name, variables string
				if err := storedRows.Scan(&email, &name, &variables); err != nil {
					storedRows.Close()
					return err
				}
				row, exists := wanted[email]
				if !exists || row.name != name || !sameJSON(row.variables, variables) {
					storedRows.Close()
					return fmt.Errorf("conflicting recipients for idempotency token %q", request.IdempotencyToken)
				}
				seen++
			}
			err = storedRows.Err()
			storedRows.Close()
			if err != nil {
				return err
			}
			if seen != len(wanted) {
				return fmt.Errorf("conflicting recipients for idempotency token %q", request.IdempotencyToken)
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err := tx.Exec(`
			INSERT INTO email_requests (
				idempotency_token, message_id, sender_email, sender_name, subject,
				template_id, text, variables, headers
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, request.IdempotencyToken, messageID, request.SenderEmail, request.SenderName, request.Subject,
			request.TemplateID, request.Text, commonVariables, headers); err != nil {
			return fmt.Errorf("insert email request: %w", err)
		}

		for _, row := range rows {
			if err := tx.Exec(`
				INSERT INTO email_progress (
					idempotency_token, recipient, recipient_name, state, variables
				) VALUES (?, ?, ?, ?, ?)
			`, request.IdempotencyToken, row.email, row.name, StatePending, row.variables); err != nil {
				return fmt.Errorf("insert progress for %q: %w", row.email, err)
			}
		}
		return nil
	}, nil
}

func sameJSON(first, second string) bool {
	var firstValue, secondValue any
	return json.Unmarshal([]byte(first), &firstValue) == nil &&
		json.Unmarshal([]byte(second), &secondValue) == nil &&
		reflect.DeepEqual(firstValue, secondValue)
}

// request reads the operation-wide data for a Send operation.
func (s *Store) request(ctx context.Context, token string) (durableRequest, error) {
	var found durableRequest
	var variables, headers string
	if err := s.db.QueryRowContext(ctx, `
		SELECT message_id, sender_email, sender_name, subject, template_id, text, variables, headers
		FROM email_requests
		WHERE idempotency_token = ?
	`, token).Scan(&found.messageID, &found.senderEmail, &found.senderName, &found.subject, &found.templateID, &found.text, &variables, &headers); err != nil {
		return durableRequest{}, err
	}

	if err := json.Unmarshal([]byte(variables), &found.variables); err != nil {
		return durableRequest{}, fmt.Errorf("decode request variables: %w", err)
	}
	if err := json.Unmarshal([]byte(headers), &found.headers); err != nil {
		return durableRequest{}, fmt.Errorf("decode request headers: %w", err)
	}
	return found, nil
}

// pendingRecipients lists the recipients still eligible for submission.
func (s *Store) pendingRecipients(ctx context.Context, token string) ([]durableRecipient, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT recipient, recipient_name, variables
		FROM email_progress
		WHERE idempotency_token = ? AND state = ?
		ORDER BY recipient
	`, token, StatePending)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pending []durableRecipient
	for rows.Next() {
		var recipient durableRecipient
		var variables string
		if err := rows.Scan(&recipient.email, &recipient.name, &variables); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(variables), &recipient.variables); err != nil {
			return nil, fmt.Errorf("decode variables for %q: %w", recipient.email, err)
		}
		pending = append(pending, recipient)
	}
	return pending, rows.Err()
}

// markSubmitted records the submission time of every pending recipient.
func (s *Store) markSubmitted(ctx context.Context, token string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE email_progress
		SET submitted_at = ?
		WHERE idempotency_token = ? AND state = ?
	`, at, token, StatePending)
	return err
}

// verifyCandidate is a submitted recipient whose acceptance is still unknown.
type verifyCandidate struct {
	recipient   string
	submittedAt time.Time
}

func (s *Store) recoveryCandidates(ctx context.Context, token string) ([]verifyCandidate, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT recipient, submitted_at
		FROM email_progress
		WHERE idempotency_token = ? AND state = ?
		ORDER BY recipient
	`, token, StateRecovery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var candidates []verifyCandidate
	for rows.Next() {
		var candidate verifyCandidate
		if err := rows.Scan(&candidate.recipient, &candidate.submittedAt); err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate)
	}
	return candidates, rows.Err()
}

func (s *Store) waitingRecipients(ctx context.Context, token string) ([]verifyCandidate, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT recipient, submitted_at FROM email_progress
		WHERE idempotency_token = ? AND state = ? ORDER BY recipient
	`, token, StateRecoveryWaiting)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var candidates []verifyCandidate
	for rows.Next() {
		var candidate verifyCandidate
		if err := rows.Scan(&candidate.recipient, &candidate.submittedAt); err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate)
	}
	return candidates, rows.Err()
}

func (s *Store) waitingRecipient(ctx context.Context, token, recipient string) (time.Time, bool, error) {
	var submittedAt time.Time
	err := s.db.QueryRowContext(ctx, `
		SELECT submitted_at FROM email_progress
		WHERE idempotency_token = ? AND recipient = ? AND state = ?
	`, token, recipient, StateRecoveryWaiting).Scan(&submittedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	return submittedAt, err == nil, err
}

func (s *Store) hasUnresolved(ctx context.Context, token string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM email_progress
		WHERE idempotency_token = ? AND state IN (?, ?)
	`, token, StateRecovery, StateRecoveryWaiting).Scan(&count)
	return count > 0, err
}

func (s *Store) recoverWork(token string) cellar.ApplicationWork {
	return func(tx cellar.ApplicationTx) error {
		return tx.Exec(`
			UPDATE email_progress SET state = ?
			WHERE idempotency_token = ? AND state = ? AND submitted_at IS NOT NULL
		`, StateRecovery, token, StatePending)
	}
}

func (s *Store) waitWork(token string) cellar.ApplicationWork {
	return func(tx cellar.ApplicationTx) error {
		return tx.Exec(`
			UPDATE email_progress SET state = ?
			WHERE idempotency_token = ? AND state = ?
		`, StateRecoveryWaiting, token, StateRecovery)
	}
}

func (s *Store) absentWork(token, recipient string) cellar.ApplicationWork {
	return func(tx cellar.ApplicationTx) error {
		return tx.Exec(`
			UPDATE email_progress SET state = ?, submitted_at = NULL
			WHERE idempotency_token = ? AND recipient = ? AND state = ?
		`, StatePending, token, recipient, StateRecoveryWaiting)
	}
}

// acceptWork records provider acceptance atomically with cell completion.
func (s *Store) acceptWork(token, recipient, pmuid string) cellar.ApplicationWork {
	return func(tx cellar.ApplicationTx) error {
		return tx.Exec(`
			UPDATE email_progress
			SET state = ?, pmuid = ?
			WHERE idempotency_token = ? AND recipient = ? AND state = ?
		`, StateAccepted, pmuid, token, recipient, StatePending)
	}
}

func (s *Store) acceptRecoveredWork(token, recipient, pmuid string) cellar.ApplicationWork {
	return func(tx cellar.ApplicationTx) error {
		return tx.Exec(`
			UPDATE email_progress SET state = ?, pmuid = ?
			WHERE idempotency_token = ? AND recipient = ? AND state = ?
		`, StateAccepted, pmuid, token, recipient, StateRecoveryWaiting)
	}
}

// marshalObject encodes a map as a JSON object, storing {} for an empty map.
func marshalObject(value any) (string, error) {
	if value == nil {
		return "{}", nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	if string(encoded) == "null" {
		return "{}", nil
	}
	return string(encoded), nil
}

func newMessageID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate message ID: %w", err)
	}
	return hex.EncodeToString(value[:]), nil
}
