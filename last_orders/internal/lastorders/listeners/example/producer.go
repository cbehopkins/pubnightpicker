package example

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"cellar/pkg/cellar"
)

type Producer struct {
	store       cellar.Store
	handlerName cellar.HandlerName
	payload     []byte
	notBefore   *time.Time
	logger      *slog.Logger
	once        sync.Once
}

func NewProducer(store cellar.Store, handlerName cellar.HandlerName, payload []byte, notBefore *time.Time, logger *slog.Logger) *Producer {
	return &Producer{store: store, handlerName: handlerName, payload: payload, notBefore: notBefore, logger: logger}
}

func (p *Producer) Start(ctx context.Context) error {
	_ = ctx
	if p.store == nil {
		return fmt.Errorf("store is required")
	}
	if p.handlerName == "" {
		return fmt.Errorf("handler name is required")
	}

	var startErr error
	p.once.Do(func() {
		ids, err := p.store.Add([]cellar.CellRequest{{
			HandlerName: p.handlerName,
			Payload:     p.payload,
			NotBefore:   p.notBefore,
		}})
		if err != nil {
			startErr = err
			return
		}
		if p.logger != nil && len(ids) > 0 {
			p.logger.Info("example listener created cell", "cell_id", ids[0], "handler", p.handlerName)
		}
	})
	return startErr
}
