package ratelimit

import (
	"fmt"
	"math"
	"sync"
	"time"
)

// TokenSource grants a token or reports how long to wait before retrying.
type TokenSource interface {
	Acquire() int
	AcquireN(count int) (int, error)
}

// RequestExceedsCapacityError identifies a request that cannot fit in one day.
type RequestExceedsCapacityError struct {
	Source  string
	Count   int
	Maximum int
}

func (e *RequestExceedsCapacityError) Error() string {
	return fmt.Sprintf("rate limit source %q request %d exceeds daily capacity %d", e.Source, e.Count, e.Maximum)
}

// Source is an in-memory daily token source.
type Source struct {
	mu          sync.Mutex
	name        string
	maximum     int
	location    *time.Location
	onExhausted func()
	now         func() time.Time
	period      string
	remaining   int
}

// New constructs a new in-memory daily token source.
func New(name string, maximum int, location *time.Location, onExhausted func()) (*Source, error) {
	return newSource(name, maximum, location, onExhausted, time.Now)
}

func newSource(name string, maximum int, location *time.Location, onExhausted func(), now func() time.Time) (*Source, error) {
	if name == "" {
		return nil, fmt.Errorf("rate limit source name is empty")
	}
	if maximum <= 0 {
		return nil, fmt.Errorf("rate limit source maximum must be positive")
	}
	if location == nil {
		return nil, fmt.Errorf("rate limit source location is nil")
	}
	if now == nil {
		return nil, fmt.Errorf("rate limit source clock is nil")
	}

	return &Source{
		name:        name,
		maximum:     maximum,
		location:    location,
		onExhausted: onExhausted,
		now:         now,
	}, nil
}

// Acquire consumes one token when available. When exhausted, it returns the
// ceiling-rounded number of seconds until the next local midnight.
func (s *Source) Acquire() int {
	wait, _ := s.AcquireN(1)
	return wait
}

// AcquireN consumes count tokens atomically, or returns a wait without consuming any.
func (s *Source) AcquireN(count int) (int, error) {
	if count <= 0 {
		return 0, fmt.Errorf("rate limit token count must be positive")
	}
	if count > s.maximum {
		return 0, &RequestExceedsCapacityError{Source: s.name, Count: count, Maximum: s.maximum}
	}
	s.mu.Lock()
	now := s.now().In(s.location)
	period := now.Format(time.DateOnly)
	if s.period != period {
		s.period = period
		s.remaining = s.maximum
	}

	if s.remaining < count {
		wait := secondsUntilNextMidnight(now, s.location)
		s.mu.Unlock()
		return wait, nil
	}

	s.remaining -= count
	exhausted := s.remaining == 0 && s.onExhausted != nil
	onExhausted := s.onExhausted
	s.mu.Unlock()

	if exhausted {
		onExhausted()
	}
	return 0, nil
}

func secondsUntilNextMidnight(now time.Time, location *time.Location) int {
	local := now.In(location)
	nextMidnight := time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, location)
	seconds := int(math.Ceil(nextMidnight.Sub(local).Seconds()))
	if seconds < 1 {
		return 1
	}
	return seconds
}
