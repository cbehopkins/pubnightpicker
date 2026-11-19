package cellar

import "time"

// Clock supplies the current application instant.
type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now()
}

func clockOrSystem(clock Clock) Clock {
	if clock == nil {
		return systemClock{}
	}
	return clock
}
