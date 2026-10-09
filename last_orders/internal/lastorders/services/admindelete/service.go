package admindelete

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"cellar/pkg/cellar"
	firebaseauthsdk "firebase.google.com/go/v4/auth"
	"google.golang.org/api/googleapi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"last_orders/internal/lastorders/truths"
)

const (
	HandlerEvaluateRequest cellar.HandlerName = "admindelete.evaluate_request"
	HandlerPersistOutcome  cellar.HandlerName = "admindelete.persist_outcome"
	retryDelay                                = 5 * time.Second
)

type Outcome string

const (
	OutcomeInvalidRequest     Outcome = "invalid_request"
	OutcomeFailedPrecondition Outcome = "failed_precondition"
	OutcomeDeleteBlocked      Outcome = "auth_delete_blocked"
	OutcomeDryRunValidated    Outcome = "dry_run_validated"
	OutcomeAuthDeleted        Outcome = "auth_deleted"
	OutcomeAuthDeleteFailed   Outcome = "auth_delete_failed"
)

type AuthClient interface {
	DeleteUser(context.Context, string) error
}

type Repository interface {
	RequestStatus(context.Context, string) (string, bool, error)
	IsPaused(context.Context) (bool, error)
	ApplicationDataExists(context.Context, string) (usersDocExists, publicDocExists bool, err error)
	PersistOutcome(context.Context, OutcomePayload) error
}

type Options struct {
	Enabled              bool
	DryRun               bool
	EnableRealAuthDelete bool
	Repository           Repository
	Auth                 AuthClient
	Logger               *slog.Logger
}

type Evaluator struct {
	enabled              bool
	dryRun               bool
	enableRealAuthDelete bool
	repository           Repository
	auth                 AuthClient
	logger               *slog.Logger
}

type OutcomePayload struct {
	RequestID           string    `json:"request_id"`
	TargetUID           string    `json:"target_uid"`
	RequestedByUID      string    `json:"requested_by_uid"`
	Reason              string    `json:"reason"`
	Outcome             Outcome   `json:"outcome"`
	Error               string    `json:"error,omitempty"`
	Idempotent          bool      `json:"idempotent"`
	RealDelete          bool      `json:"real_delete"`
	UsersDocExists      bool      `json:"users_doc_exists"`
	UserPublicDocExists bool      `json:"user_public_doc_exists"`
	At                  time.Time `json:"at"`
}

func New(options Options) (*Evaluator, error) {
	if options.Repository == nil {
		return nil, fmt.Errorf("admin-delete repository is required")
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	if options.Enabled && !options.DryRun && options.EnableRealAuthDelete && options.Auth == nil {
		return nil, fmt.Errorf("firebase auth delete client is required when real deletion is enabled")
	}
	return &Evaluator{
		enabled:              options.Enabled,
		dryRun:               options.DryRun,
		enableRealAuthDelete: options.EnableRealAuthDelete,
		repository:           options.Repository,
		auth:                 options.Auth,
		logger:               options.Logger,
	}, nil
}

func (e *Evaluator) Handle(ctx context.Context, request truths.AdminDeleteRequested) cellar.Result {
	if !e.enabled {
		return retryLater(e.logger, "admin-delete processing disabled", nil)
	}
	if request.Identity() == "" {
		return cellar.Complete{}
	}
	status, exists, err := e.repository.RequestStatus(ctx, request.Request.RequestID)
	if err != nil {
		return retryLater(e.logger, "read current admin-delete request", err)
	}
	realDelete := !e.dryRun && e.enableRealAuthDelete
	if !exists || (status != "pending" && !(realDelete && status == string(OutcomeDryRunValidated))) {
		return cellar.Complete{}
	}
	paused, err := e.repository.IsPaused(ctx)
	if err != nil {
		return retryLater(e.logger, "read admin-delete kill switch", err)
	}
	if paused {
		return cellar.RetrySequence{Delay: time.Minute}
	}

	snapshot := request.Request
	outcome := OutcomePayload{
		RequestID:      snapshot.RequestID,
		TargetUID:      snapshot.TargetUID,
		RequestedByUID: snapshot.RequestedByUID,
		Reason:         snapshot.Reason,
		At:             time.Now().UTC(),
		RealDelete:     realDelete,
	}
	if snapshot.SchemaVersion != 1 || strings.TrimSpace(snapshot.TargetUID) == "" {
		outcome.Outcome = OutcomeInvalidRequest
		return e.persistCell(outcome)
	}
	usersDocExists, publicDocExists, err := e.repository.ApplicationDataExists(ctx, snapshot.TargetUID)
	if err != nil {
		return retryLater(e.logger, "check admin-delete application-data preconditions", err)
	}
	outcome.UsersDocExists = usersDocExists
	outcome.UserPublicDocExists = publicDocExists
	if usersDocExists || publicDocExists {
		outcome.Outcome = OutcomeFailedPrecondition
		return e.persistCell(outcome)
	}
	if e.dryRun {
		outcome.Outcome = OutcomeDryRunValidated
		return e.persistCell(outcome)
	}
	if !e.enableRealAuthDelete {
		outcome.Outcome = OutcomeDeleteBlocked
		return e.persistCell(outcome)
	}
	if err := e.auth.DeleteUser(ctx, snapshot.TargetUID); err != nil {
		if isUserNotFound(err) {
			outcome.Outcome = OutcomeAuthDeleted
			outcome.Idempotent = true
			return e.persistCell(outcome)
		}
		if permanentDeleteError(err) {
			outcome.Outcome = OutcomeAuthDeleteFailed
			outcome.Error = err.Error()
			return e.persistCell(outcome)
		}
		return retryLater(e.logger, "delete Firebase Auth user", err)
	}
	outcome.Outcome = OutcomeAuthDeleted
	return e.persistCell(outcome)
}

func (e *Evaluator) persistCell(payload OutcomePayload) cellar.Result {
	definition, err := cellar.NewCellDefinition(HandlerPersistOutcome, payload)
	if err != nil {
		return cellar.ErrorResult{Message: "build admin-delete persistence Cell", Err: err}
	}
	request, err := definition.CellRequest()
	if err != nil {
		return cellar.ErrorResult{Message: "encode admin-delete persistence Cell", Err: err}
	}
	return cellar.Complete{NewCells: []cellar.CellRequest{request}}
}

type PersistenceHandler struct {
	Repository Repository
	Logger     *slog.Logger
}

func (h PersistenceHandler) Handle(ctx context.Context, payload OutcomePayload) cellar.Result {
	if h.Repository == nil {
		return cellar.ErrorResult{Message: "admin-delete repository is required"}
	}
	if err := h.Repository.PersistOutcome(ctx, payload); err != nil {
		return retryLater(h.Logger, "persist admin-delete request outcome and audit", err)
	}
	if h.Logger != nil {
		h.Logger.Info("admin-delete outcome persisted", "request_id", payload.RequestID, "outcome", payload.Outcome)
	}
	return cellar.Complete{}
}

func retryLater(logger *slog.Logger, message string, err error) cellar.Result {
	if logger != nil {
		if err != nil {
			logger.Error(message, "err", err)
		} else {
			logger.Warn(message)
		}
	}
	if err != nil {
		return cellar.RetrySequence{Delay: retryDelay}
	}
	return cellar.RetrySequence{Delay: time.Minute}
}

func permanentDeleteError(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *googleapi.Error
	if errors.As(err, &apiErr) {
		return apiErr.Code >= 400 && apiErr.Code < 500 && apiErr.Code != 404 && apiErr.Code != 408 && apiErr.Code != 429
	}
	switch status.Code(err) {
	case codes.InvalidArgument, codes.Unauthenticated, codes.PermissionDenied, codes.FailedPrecondition:
		return true
	default:
		return false
	}
}

func isUserNotFound(err error) bool {
	if firebaseauthsdk.IsUserNotFound(err) {
		return true
	}
	var apiErr *googleapi.Error
	if errors.As(err, &apiErr) && apiErr.Code == 404 {
		return true
	}
	return status.Code(err) == codes.NotFound
}
