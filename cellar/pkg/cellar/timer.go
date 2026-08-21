package cellar

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// TimerMode controls how a timer calculates its next deadline.
type TimerMode string

const (
	// TimerFixedDelay schedules the next run relative to callback completion.
	TimerFixedDelay TimerMode = "fixed-delay"
	// TimerFixedRate schedules against the previous deadline and coalesces missed ticks.
	TimerFixedRate TimerMode = "fixed-rate"
)

// TimerConfig defines a durable timer's recurrence behaviour.
type TimerConfig struct {
	Interval time.Duration
	Mode     TimerMode
}

// TimerCallback is application work invoked when a durable timer fires.
// Returning an error cancels and deletes the timer.
type TimerCallback func(context.Context) error

// Timer defines one uniquely named durable recurring timer.
type Timer struct {
	name     HandlerName
	config   TimerConfig
	callback TimerCallback
}

// NewTimer constructs a durable timer definition.
func NewTimer(name HandlerName, config TimerConfig, callback TimerCallback) (*Timer, error) {
	if name == "" {
		return nil, ErrHandlerNameRequired
	}
	if config.Interval <= 0 {
		return nil, ErrTimerIntervalInvalid
	}
	if config.Mode != TimerFixedDelay && config.Mode != TimerFixedRate {
		return nil, ErrTimerModeInvalid
	}
	if callback == nil {
		return nil, ErrTimerCallbackNil
	}
	return &Timer{name: name, config: config, callback: callback}, nil
}

// Register binds the timer's durable name to its process-local callback.
// Applications must register the same name on every startup before Cellar starts.
func (t *Timer) Register(c *Cellar) error {
	if c == nil {
		return ErrCellarNil
	}
	if t == nil {
		return ErrTimerNil
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.started {
		return ErrCellarStarted
	}
	err := c.registry.Register(t.name, timerRegistration{callback: t.callback})
	if errors.Is(err, ErrHandlerAlreadyRegistered) {
		return fmt.Errorf("%w: %s", ErrTimerAlreadyExists, t.name)
	}
	return err
}

// Schedule creates the timer's first durable occurrence.
// Scheduling an active timer with the same name returns ErrTimerAlreadyExists.
func (t *Timer) Schedule(c *Cellar) (CellID, error) {
	if c == nil {
		return "", ErrCellarNil
	}
	if t == nil {
		return "", ErrTimerNil
	}
	if c.store == nil {
		return "", ErrStoreNil
	}

	payload, err := marshalJSON(timerPayload(t.config))
	if err != nil {
		return "", fmt.Errorf("encode timer payload: %w", err)
	}
	id := timerCellID(t.name)
	due := time.Now().Add(t.config.Interval)
	err = c.store.AddWithIDs([]IdentifiedCellRequest{{
		ID: id,
		CellRequest: CellRequest{
			HandlerName: t.name,
			Payload:     payload,
			NotBefore:   &due,
		},
	}})
	if errors.Is(err, ErrCellAlreadyExists) {
		return "", fmt.Errorf("%w: %s", ErrTimerAlreadyExists, t.name)
	}
	if err != nil {
		return "", err
	}
	return id, nil
}

type timerPayload TimerConfig

type timerRegistration struct {
	callback TimerCallback
}

func (r timerRegistration) Execute(ctx context.Context, cell Cell) Result {
	var payload timerPayload
	if err := unmarshalJSON(cell.Payload, &payload); err != nil {
		return ErrorResult{Message: "decode timer payload", Err: err}
	}
	if r.callback == nil {
		return ErrorResult{Message: "execute timer callback", Err: ErrTimerCallbackNil}
	}
	if err := r.callback(ctx); err != nil {
		return Complete{}
	}

	now := time.Now()
	next := now.Add(payload.Interval)
	if payload.Mode == TimerFixedRate && cell.NotBefore != nil {
		next = nextFixedRateDeadline(*cell.NotBefore, now, payload.Interval)
	}
	return Retry{NotBefore: &next}
}

func (r timerRegistration) Inspect(cell Cell) Inspection {
	var payload timerPayload
	err := unmarshalJSON(cell.Payload, &payload)
	return Inspection{
		Cell:          cloneCell(cell),
		Payload:       TimerConfig(payload),
		PayloadFormat: "json",
		DecodeError:   err,
	}
}

func nextFixedRateDeadline(previous, now time.Time, interval time.Duration) time.Time {
	next := previous.Add(interval)
	if next.After(now) {
		return next
	}
	missed := now.Sub(previous)/interval + 1
	return previous.Add(missed * interval)
}

func timerCellID(name HandlerName) CellID {
	return CellID("timer:" + string(name))
}

var (
	ErrCellarNil            = errors.New("cellar is nil")
	ErrTimerNil             = errors.New("timer is nil")
	ErrTimerIntervalInvalid = errors.New("timer interval must be positive")
	ErrTimerModeInvalid     = errors.New("timer mode is invalid")
	ErrTimerCallbackNil     = errors.New("timer callback is nil")
	ErrTimerAlreadyExists   = errors.New("timer already exists")
)
