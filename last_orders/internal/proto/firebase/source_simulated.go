package firebase

import "time"

// SimulatedSource is a deterministic in-memory source for integration tests.
type SimulatedSource struct {
	Events   []Event
	Interval time.Duration
}

func (s SimulatedSource) Run(ctxDone <-chan struct{}, out chan<- Event) error {
	interval := s.Interval
	if interval <= 0 {
		interval = 10 * time.Millisecond
	}

	for _, event := range s.Events {
		select {
		case <-ctxDone:
			return nil
		case out <- event:
		}
		select {
		case <-ctxDone:
			return nil
		case <-time.After(interval):
		}
	}
	return nil
}
