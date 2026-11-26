package admindelete

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/services/votecleanup"
	"last_orders/internal/lastorders/truths"

	firebaseauthsdk "firebase.google.com/go/v4/auth"
	"google.golang.org/api/googleapi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	HandlerEvaluateRequest cellar.HandlerName = "admindelete.evaluate_request"
	HandlerPreviewRequest  cellar.HandlerName = "admindelete.preview_request"
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

type VoteCleaner interface {
	RemoveUser(context.Context, string) (votecleanup.Result, error)
}

type previewCleaner interface {
	PreviewUser(context.Context, string) ([]string, error)
}

type Repository interface {
	RequestStatus(context.Context, string) (string, bool, error)
	IsPaused(context.Context) (bool, error)
	ApplicationDataExists(context.Context, string) (usersDocExists, publicDocExists bool, err error)
	CleanApplicationData(context.Context, string, bool, *slog.Logger) error
	PersistOutcome(context.Context, OutcomePayload) error
}

type Options struct {
	Enabled              bool
	DryRun               bool
	EnableRealAuthDelete bool
	Repository           Repository
	Auth                 AuthClient
	VoteCleaner          VoteCleaner
	AttendanceCleaner    VoteCleaner
	Logger               *slog.Logger
}

type Evaluator struct {
	enabled              bool
	dryRun               bool
	enableRealAuthDelete bool
	repository           Repository
	auth                 AuthClient
	voteCleaner          VoteCleaner
	attendanceCleaner    VoteCleaner
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
	VotesDocsUpdated    int       `json:"votes_docs_updated"`
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
		voteCleaner:          options.VoteCleaner,
		attendanceCleaner:    options.AttendanceCleaner,
		logger:               options.Logger,
	}, nil
}

func (e *Evaluator) Handle(ctx context.Context, request truths.AdminDeleteRequested) cellar.Result {
	if e.dryRun {
		return retryLater(e.logger, "defer live admin-delete work during dry-run", nil)
	}
	return e.handle(ctx, request, false)
}

type PreviewHandler struct {
	Evaluator *Evaluator
}

func (h PreviewHandler) Handle(ctx context.Context, request truths.AdminDeleteRequested) cellar.Result {
	return h.Evaluator.handle(ctx, request, true)
}

func (e *Evaluator) handle(ctx context.Context, request truths.AdminDeleteRequested, dryRun bool) cellar.Result {
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
	realDelete := !dryRun && e.enableRealAuthDelete
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
		if dryRun {
			e.logger.Error("invalid admin-delete dry-run request", "request_id", snapshot.RequestID)
			return cellar.Complete{}
		}
		outcome.Outcome = OutcomeInvalidRequest
		return e.persistCell(outcome)
	}
	if dryRun {
		for _, cleaner := range []VoteCleaner{e.voteCleaner, e.attendanceCleaner} {
			if cleaner == nil {
				continue
			}
			preview, ok := cleaner.(previewCleaner)
			if !ok {
				return retryLater(e.logger, "preview admin account cleanup", fmt.Errorf("cleaner does not support preview"))
			}
			paths, err := preview.PreviewUser(ctx, snapshot.TargetUID)
			if err != nil {
				return retryLater(e.logger, "preview admin account cleanup", err)
			}
			for _, path := range paths {
				e.logger.Info("admin-delete dry-run", "action", "remove user", "entity", path)
			}
		}
		if err := e.repository.CleanApplicationData(ctx, snapshot.TargetUID, true, e.logger); err != nil {
			return retryLater(e.logger, "preview admin account application data", err)
		}
		e.logger.Info("admin-delete dry-run", "action", "delete Auth account", "entity", snapshot.TargetUID)
		return cellar.Complete{}
	}
	if !e.enableRealAuthDelete {
		outcome.Outcome = OutcomeDeleteBlocked
		return e.persistCell(outcome)
	}
	if e.voteCleaner != nil {
		voteResult, err := e.voteCleaner.RemoveUser(ctx, snapshot.TargetUID)
		if err != nil {
			return retryLater(e.logger, "clean Firebase votes before admin account deletion", err)
		}
		outcome.VotesDocsUpdated = voteResult.UpdatedDocuments
	}
	if e.attendanceCleaner != nil {
		if _, err := e.attendanceCleaner.RemoveUser(ctx, snapshot.TargetUID); err != nil {
			return retryLater(e.logger, "clean Firebase attendance before admin account deletion", err)
		}
	}
	if err := e.repository.CleanApplicationData(ctx, snapshot.TargetUID, false, e.logger); err != nil {
		return retryLater(e.logger, "clean Firebase application data before admin account deletion", err)
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
	DryRun     bool
}

func (h PersistenceHandler) Handle(ctx context.Context, payload OutcomePayload) cellar.Result {
	if h.DryRun {
		return retryLater(h.Logger, "defer admin-delete outcome during dry-run", nil)
	}
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
