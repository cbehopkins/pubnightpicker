package admindelete

import (
	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/services/admindelete"
	"last_orders/internal/lastorders/truths"
)

func Register() {
	truths.AdminDeleteRequestedRegistry.Register(admindelete.HandlerEvaluateRequest)
	truths.AdminDeletePreviewRegistry.Register(admindelete.HandlerPreviewRequest)
}

func RegisterHandlers(runtime *cellar.Cellar, evaluator *admindelete.Evaluator, persistence admindelete.PersistenceHandler) error {
	if runtime == nil {
		return cellar.ErrCellarNil
	}
	if evaluator == nil {
		return nil
	}
	if err := runtime.Register(admindelete.HandlerEvaluateRequest, evaluator); err != nil {
		return err
	}
	if err := runtime.Register(admindelete.HandlerPreviewRequest, admindelete.PreviewHandler{Evaluator: evaluator}); err != nil {
		return err
	}
	return runtime.Register(admindelete.HandlerPersistOutcome, persistence)
}
