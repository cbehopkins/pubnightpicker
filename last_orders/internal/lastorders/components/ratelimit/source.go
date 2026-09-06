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

func NewSource(name string, maximum int, location *time.Location, onExhausted func()) (*Source, error) {
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
	s.mu.Lock()
	now := s.now().In(s.location)
	period := now.Format(time.DateOnly)
	if s.period != period {
		s.period = period
		s.remaining = s.maximum
	}

	if s.remaining == 0 {
		wait := secondsUntilNextMidnight(now, s.location)
		s.mu.Unlock()
		return wait
	}

	s.remaining--
	exhausted := s.remaining == 0 && s.onExhausted != nil
	onExhausted := s.onExhausted
	s.mu.Unlock()

	if exhausted {
		onExhausted()
	}
	return 0
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
