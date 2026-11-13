package cellar

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// TimerMode controls how a timer calculates its next deadline.
type TimerMode string

const (
	// TimerFixedDelay schedules the next run relative to callback completion.
	TimerFixedDelay TimerMode = "fixed-delay"
	// TimerFixedRate schedules against the previous deadline and coalesces missed ticks.
	TimerFixedRate TimerMode = "fixed-rate"
	// TimerDailyCalendar schedules once per local calendar day at DailyAt in Location.
	TimerDailyCalendar TimerMode = "daily-calendar"
)

// TimerConfig defines a durable timer's recurrence behaviour.
type TimerConfig struct {
	Interval time.Duration
	Mode     TimerMode
	DailyAt  string
	Location string
}

// TimerCallback is application work invoked when a durable timer fires.
// Returning an error cancels and deletes the timer.
type TimerCallback func(context.Context) error

// Timer defines one uniquely named durable recurring timer.
type Timer struct {
	name     HandlerName
	config   TimerConfig
	callback TimerCallback
	producer *timerProducer
}

// NewTimer constructs a durable timer definition.
func NewTimer(name HandlerName, config TimerConfig, callback TimerCallback) (*Timer, error) {
	if name == "" {
		return nil, ErrHandlerNameRequired
	}
	if config.Mode != TimerFixedDelay && config.Mode != TimerFixedRate && config.Mode != TimerDailyCalendar {
		return nil, ErrTimerModeInvalid
	}
	if config.Mode == TimerDailyCalendar {
		if _, _, err := parseDailyAt(config.DailyAt); err != nil {
			return nil, ErrTimerDailyAtInvalid
		}
		if _, err := time.LoadLocation(config.Location); err != nil {
			return nil, ErrTimerLocationInvalid
		}
	} else if config.Interval <= 0 {
		return nil, ErrTimerIntervalInvalid
	}
	if callback == nil {
		return nil, ErrTimerCallbackNil
	}
	return &Timer{name: name, config: config, callback: callback, producer: &timerProducer{}}, nil
}

// StopProduction closes callback admission and prevents subsequent occurrences
// from being scheduled. An admitted callback may finish, but its retry is
// committed only if it wins the producer lock before this method closes it.
func (t *Timer) StopProduction() {
	if t == nil || t.producer == nil {
		return
	}
	t.producer.mu.Lock()
	t.producer.stopped = true
	t.producer.mu.Unlock()
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
	err := c.registry.Register(t.name, timerRegistration{callback: t.callback, producer: t.producer})
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
	t.producer.mu.Lock()
	defer t.producer.mu.Unlock()
	if t.producer.stopped {
		return "", ErrTimerProductionStopped
	}

	payload, err := marshalJSON(timerPayload(t.config))
	if err != nil {
		return "", fmt.Errorf("encode timer payload: %w", err)
	}
	id := timerCellID(t.name)
	due := time.Now().Add(t.config.Interval)
	if t.config.Mode == TimerDailyCalendar {
		due, err = nextCalendarDeadline(time.Now(), t.config)
		if err != nil {
			return "", fmt.Errorf("calculate calendar timer deadline: %w", err)
		}
	}
	_, err = c.store.Add([]CellRequest{{
		ID:        id,
		Steps:     []CellStep{{HandlerName: t.name, Payload: payload}},
		NotBefore: &due,
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
	producer *timerProducer
}

func (r timerRegistration) Execute(ctx context.Context, cell Cell) Result {
	var payload timerPayload
	if err := unmarshalJSON(currentStepPayload(cell), &payload); err != nil {
		return ErrorResult{Message: "decode timer payload", Err: err}
	}
	if r.callback == nil {
		return ErrorResult{Message: "execute timer callback", Err: ErrTimerCallbackNil}
	}
	lease, admitted := r.producer.admit()
	if !admitted {
		return Complete{}
	}
	if err := r.callback(ctx); err != nil {
		return Complete{}
	}

	now := time.Now()
	next := now.Add(payload.Interval)
	if payload.Mode == TimerDailyCalendar {
		calendarNext, err := nextCalendarDeadline(now, TimerConfig(payload))
		if err != nil {
			return ErrorResult{Message: "calculate calendar timer deadline", Err: err}
		}
		next = calendarNext
	} else if payload.Mode == TimerFixedRate && cell.NotBefore != nil {
		next = nextFixedRateDeadline(*cell.NotBefore, now, payload.Interval)
	}
	return Retry{NotBefore: &next, timerLease: lease}
}

type timerProducer struct {
	mu      sync.Mutex
	stopped bool
}

type timerLease struct {
	producer *timerProducer
}

func (p *timerProducer) admit() (*timerLease, bool) {
	if p == nil {
		return nil, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		return nil, false
	}
	return &timerLease{producer: p}, true
}

func (l *timerLease) applyRetry(applier *StoreResultApplier, ctx context.Context, cell Cell, retry Retry) error {
	p := l.producer
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		return applier.applyResult(ctx, cell, Complete{})
	}
	retry.timerLease = nil
	return applier.applyResult(ctx, cell, retry)
}

func (r timerRegistration) Inspect(cell Cell) Inspection {
	var payload timerPayload
	err := unmarshalJSON(currentStepPayload(cell), &payload)
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

func nextCalendarDeadline(now time.Time, config TimerConfig) (time.Time, error) {
	hour, minute, err := parseDailyAt(config.DailyAt)
	if err != nil {
		return time.Time{}, err
	}
	location, err := time.LoadLocation(config.Location)
	if err != nil {
		return time.Time{}, err
	}
	localNow := now.In(location)
	deadline := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), hour, minute, 0, 0, location)
	if !deadline.After(localNow) {
		deadline = time.Date(localNow.Year(), localNow.Month(), localNow.Day()+1, hour, minute, 0, 0, location)
	}
	return deadline, nil
}

func parseDailyAt(value string) (int, int, error) {
	parsed, err := time.Parse("15:04", value)
	if err != nil {
		return 0, 0, err
	}
	return parsed.Hour(), parsed.Minute(), nil
}

func timerCellID(name HandlerName) CellID {
	return CellID("timer:" + string(name))
}

var (
	ErrCellarNil              = errors.New("cellar is nil")
	ErrTimerNil               = errors.New("timer is nil")
	ErrTimerIntervalInvalid   = errors.New("timer interval must be positive")
	ErrTimerModeInvalid       = errors.New("timer mode is invalid")
	ErrTimerDailyAtInvalid    = errors.New("timer daily time must use HH:MM")
	ErrTimerLocationInvalid   = errors.New("timer location is invalid")
	ErrTimerCallbackNil       = errors.New("timer callback is nil")
	ErrTimerAlreadyExists     = errors.New("timer already exists")
	ErrTimerProductionStopped = errors.New("timer production is stopped")
)
