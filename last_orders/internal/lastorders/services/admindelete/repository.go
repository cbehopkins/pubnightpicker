package admindelete

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	requestCollection = "admin_delete_requests"
	auditCollection   = "admin_delete_request_audit"
	killSwitchPath    = "config/admin_delete"
)

type FirestoreRepository struct {
	client *firestore.Client
}

func NewFirestoreRepository(client *firestore.Client) (*FirestoreRepository, error) {
	if client == nil {
		return nil, fmt.Errorf("firestore client is required")
	}
	return &FirestoreRepository{client: client}, nil
}

func (r *FirestoreRepository) RequestStatus(ctx context.Context, requestID string) (string, bool, error) {
	snapshot, err := r.client.Collection(requestCollection).Doc(requestID).Get(ctx)
	if status.Code(err) == codes.NotFound {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read admin-delete request %q: %w", requestID, err)
	}
	value, exists := snapshot.Data()["status"]
	if !exists {
		return "", true, fmt.Errorf("admin-delete request %q has no status", requestID)
	}
	requestStatus, ok := value.(string)
	if !ok {
		return "", true, fmt.Errorf("admin-delete request %q status must be a string", requestID)
	}
	return requestStatus, true, nil
}

func (r *FirestoreRepository) IsPaused(ctx context.Context) (bool, error) {
	snapshot, err := r.client.Doc(killSwitchPath).Get(ctx)
	if status.Code(err) == codes.NotFound {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read admin-delete kill switch: %w", err)
	}
	value, exists := snapshot.Data()["paused"]
	if !exists {
		return false, nil
	}
	paused, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("%s.paused must be a boolean", killSwitchPath)
	}
	return paused, nil
}

func (r *FirestoreRepository) ApplicationDataExists(ctx context.Context, targetUID string) (bool, bool, error) {
	usersDocExists, err := documentExists(ctx, r.client.Collection("users").Doc(targetUID))
	if err != nil {
		return false, false, fmt.Errorf("check users/%s: %w", targetUID, err)
	}
	publicDocExists, err := documentExists(ctx, r.client.Collection("user-public").Doc(targetUID))
	if err != nil {
		return false, false, fmt.Errorf("check user-public/%s: %w", targetUID, err)
	}
	return usersDocExists, publicDocExists, nil
}

func (r *FirestoreRepository) PersistOutcome(ctx context.Context, payload OutcomePayload) error {
	if err := validateOutcome(payload.Outcome); err != nil {
		return err
	}
	requestRef := r.client.Collection(requestCollection).Doc(payload.RequestID)
	auditRef := r.client.Collection(auditCollection).Doc(payload.RequestID)
	return r.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		requestSnapshot, err := tx.Get(requestRef)
		if status.Code(err) == codes.NotFound {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read request for admin-delete outcome: %w", err)
		}
		currentStatus, ok := requestSnapshot.Data()["status"].(string)
		if !ok {
			return fmt.Errorf("admin-delete request %q status must be a string", payload.RequestID)
		}
		promoting := currentStatus == string(OutcomeDryRunValidated) && payload.RealDelete && payload.Outcome != OutcomeDryRunValidated
		if currentStatus != "pending" && !promoting {
			return nil
		}
		var dryRunEvidence map[string]any
		if promoting {
			previousAudit, err := tx.Get(auditRef)
			if err != nil {
				return fmt.Errorf("read dry-run audit before promotion: %w", err)
			}
			dryRunEvidence = previousAudit.Data()
		}
		if currentStatus == "pending" || promoting {
			updates := []firestore.Update{
				{Path: "status", Value: string(payload.Outcome)},
				{Path: "updatedAt", Value: payload.At.UTC()},
				{Path: "usersDocExists", Value: payload.UsersDocExists},
				{Path: "userPublicDocExists", Value: payload.UserPublicDocExists},
			}
			if payload.Error != "" {
				updates = append(updates, firestore.Update{Path: "lastError", Value: payload.Error})
			} else {
				updates = append(updates, firestore.Update{Path: "lastError", Value: firestore.Delete})
			}
			if payload.Outcome == OutcomeAuthDeleted {
				updates = append(updates, firestore.Update{Path: "authDeletedAt", Value: payload.At.UTC()})
			}
			if err := tx.Update(requestRef, updates); err != nil {
				return fmt.Errorf("write admin-delete request outcome: %w", err)
			}
		}
		audit := map[string]any{
			"requestId":           payload.RequestID,
			"targetUid":           payload.TargetUID,
			"requestedByUid":      payload.RequestedByUID,
			"reason":              payload.Reason,
			"outcome":             string(payload.Outcome),
			"idempotent":          payload.Idempotent,
			"usersDocExists":      payload.UsersDocExists,
			"userPublicDocExists": payload.UserPublicDocExists,
			"at":                  payload.At.UTC(),
		}
		if payload.Error != "" {
			audit["error"] = payload.Error
		}
		if dryRunEvidence != nil {
			audit["dryRunEvidence"] = dryRunEvidence
		}
		if err := tx.Set(auditRef, audit); err != nil {
			return fmt.Errorf("write admin-delete audit: %w", err)
		}
		return nil
	})
}

func documentExists(ctx context.Context, ref *firestore.DocumentRef) (bool, error) {
	_, err := ref.Get(ctx)
	if status.Code(err) == codes.NotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func validateOutcome(outcome Outcome) error {
	switch outcome {
	case OutcomeInvalidRequest, OutcomeFailedPrecondition, OutcomeDeleteBlocked,
		OutcomeDryRunValidated, OutcomeAuthDeleted, OutcomeAuthDeleteFailed:
		return nil
	default:
		return fmt.Errorf("unknown admin-delete outcome %q", outcome)
	}
}

func nowUTC() time.Time {
	return time.Now().UTC()
}
