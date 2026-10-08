package recurrence

import "time"

type Rule struct {
	Frequency string
	Date      string
	Interval  int
	Weekdays  []int
	Weekday   *int
	MonthDay  *int
	Nth       *int
	Month     *int
}

// EventVenue is the current Firestore state of a recurring event venue.
type EventVenue struct {
	ID                 string
	Name               string
	Recurrence         map[string]any
	NextOccurrenceDate string
}

type DateOnly struct {
	Year  int
	Month time.Month
	Day   int
}

func (d DateOnly) Time(loc *time.Location) time.Time {
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, loc)
}

func (d DateOnly) String() string {
	return d.Time(time.UTC).Format("2006-01-02")
}
