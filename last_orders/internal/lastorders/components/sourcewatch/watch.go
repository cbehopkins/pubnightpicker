package sourcewatch

import (
	"context"
	"sync"
)

type Stream interface {
	Stop()
}

type Watch struct {
	stream Stream
	cancel context.CancelFunc
	once   sync.Once
}

func New(cancel context.CancelFunc, stream Stream) *Watch {
	return &Watch{cancel: cancel, stream: stream}
}

func (watch *Watch) Stop() {
	if watch == nil {
		return
	}
	watch.once.Do(func() {
		watch.cancel()
		watch.stream.Stop()
	})
}
