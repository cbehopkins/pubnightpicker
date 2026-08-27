package lifecycle

// Lifecycle manages the lifecycle of a listener worker, providing start and close functionality.
// Not a listener, but used by pretty much every listener
import (
	"context"
	"errors"
	"sync"
)

// Controller manages one cancellable listener worker and waits for its exit.
type Controller struct {
	mu      sync.Mutex
	cancel  context.CancelFunc
	done    chan struct{}
	started bool
	closed  bool
}

func (c *Controller) Start(parent context.Context, worker func(context.Context)) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errors.New("listener is closed")
	}
	if c.started {
		return errors.New("listener is already started")
	}

	ctx, cancel := context.WithCancel(parent)
	c.cancel = cancel
	c.done = make(chan struct{})
	c.started = true
	go func() {
		defer close(c.done)
		worker(ctx)
	}()
	return nil
}

func (c *Controller) Close() error {
	c.mu.Lock()
	if c.closed {
		done := c.done
		c.mu.Unlock()
		if done != nil {
			<-done
		}
		return nil
	}
	c.closed = true
	cancel := c.cancel
	done := c.done
	c.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
	return nil
}
