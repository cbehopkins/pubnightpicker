package recurrence

import (
	"sort"
	"time"
)

func ParseRule(raw map[string]any) (Rule, bool) {
	if raw == nil {
		return Rule{}, false
	}

	frequency, ok := asString(raw["frequency"])
	if !ok || frequency == "" {
		return Rule{}, false
	}

	rule := Rule{Frequency: frequency, Interval: asIntDefault(raw["interval"], 1)}
	rule.Date, _ = asString(raw["date"])

	if weekdays, ok := asIntSlice(raw["weekdays"]); ok {
		rule.Weekdays = weekdays
	}
	if v, ok := asInt(raw["weekday"]); ok {
		rule.Weekday = &v
	}
	if v, ok := asInt(raw["month_day"]); ok {
		rule.MonthDay = &v
	}
	if v, ok := asInt(raw["nth"]); ok {
		rule.Nth = &v
	}
	if v, ok := asInt(raw["month"]); ok {
		rule.Month = &v
	}

	if rule.Interval <= 0 {
		rule.Interval = 1
	}
	return rule, true
}

func NextOccurrence(rule Rule, reference time.Time, loc *time.Location) (DateOnly, bool) {
	reference = beginningOfDay(reference.In(loc))

	switch rule.Frequency {
	case "once":
		occurrence, ok := parseDate(rule.Date, loc)
		if !ok {
			return DateOnly{}, false
		}
		if occurrence.Before(reference) {
			return DateOnly{}, false
		}
		return toDateOnly(occurrence), true
	case "weekly":
		return nextWeekly(rule, reference, loc)
	case "monthly":
		return nextMonthly(rule, reference, loc)
	case "yearly":
		return nextYearly(rule, reference, loc)
	default:
		return DateOnly{}, false
	}
}

func nextWeekly(rule Rule, reference time.Time, loc *time.Location) (DateOnly, bool) {
	weekdays := rule.Weekdays
	if len(weekdays) == 0 && rule.Weekday != nil {
		weekdays = []int{*rule.Weekday}
	}
	if len(weekdays) == 0 {
		return DateOnly{}, false
	}
	sort.Ints(weekdays)

	for offset := 0; offset < 370*3; offset++ {
		candidate := reference.AddDate(0, 0, offset)

		if !isWeeklyIntervalMatch(reference, candidate, rule.Interval) {
			continue
		}

		if containsWeekday(weekdays, toContractWeekday(candidate.Weekday())) {
			return toDateOnly(candidate), true
		}
	}
	return DateOnly{}, false
}

func nextMonthly(rule Rule, reference time.Time, loc *time.Location) (DateOnly, bool) {
	for step := 0; step < 600; step++ {
		candidateMonth := beginningOfMonth(reference).AddDate(0, step*rule.Interval, 0)
		candidate, ok := monthlyOccurrenceInMonth(rule, candidateMonth, loc)
		if !ok {
			continue
		}
		if candidate.Before(reference) {
			continue
		}
		return toDateOnly(candidate), true
	}
	return DateOnly{}, false
}

func nextYearly(rule Rule, reference time.Time, loc *time.Location) (DateOnly, bool) {
	if rule.Month == nil || *rule.Month < 1 || *rule.Month > 12 {
		return DateOnly{}, false
	}
	for step := 0; step < 300; step++ {
		year := reference.Year() + step*rule.Interval
		candidateMonth := time.Date(year, time.Month(*rule.Month), 1, 0, 0, 0, 0, loc)
		candidate, ok := monthlyOccurrenceInMonth(rule, candidateMonth, loc)
		if !ok {
			continue
		}
		if candidate.Before(reference) {
			continue
		}
		return toDateOnly(candidate), true
	}
	return DateOnly{}, false
}

func monthlyOccurrenceInMonth(rule Rule, monthStart time.Time, loc *time.Location) (time.Time, bool) {
	if rule.MonthDay != nil {
		day := *rule.MonthDay
		if day < 1 || day > 31 {
			return time.Time{}, false
		}
		candidate := time.Date(monthStart.Year(), monthStart.Month(), day, 0, 0, 0, 0, loc)
		if candidate.Month() != monthStart.Month() {
			return time.Time{}, false
		}
		return candidate, true
	}
	if rule.Weekday == nil || rule.Nth == nil {
		return time.Time{}, false
	}
	return nthWeekdayInMonth(monthStart.Year(), monthStart.Month(), *rule.Weekday, *rule.Nth, loc)
}

func nthWeekdayInMonth(year int, month time.Month, weekday int, nth int, loc *time.Location) (time.Time, bool) {
	if weekday < 0 || weekday > 6 || nth == 0 {
		return time.Time{}, false
	}
	matches := make([]time.Time, 0, 5)
	for d := 1; d <= 31; d++ {
		candidate := time.Date(year, month, d, 0, 0, 0, 0, loc)
		if candidate.Month() != month {
			break
		}
		if toContractWeekday(candidate.Weekday()) == weekday {
			matches = append(matches, candidate)
		}
	}
	if len(matches) == 0 {
		return time.Time{}, false
	}
	if nth == -1 {
		return matches[len(matches)-1], true
	}
	if nth < 0 || nth > len(matches) {
		return time.Time{}, false
	}
	return matches[nth-1], true
}

func beginningOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func beginningOfMonth(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
}

func isWeeklyIntervalMatch(start, candidate time.Time, interval int) bool {
	sMonday := monday(start)
	cMonday := monday(candidate)
	weeks := int(cMonday.Sub(sMonday).Hours() / (24 * 7))
	return weeks%interval == 0
}

func monday(t time.Time) time.Time {
	day := toContractWeekday(t.Weekday())
	return beginningOfDay(t).AddDate(0, 0, -day)
}

func containsWeekday(items []int, day int) bool {
	for _, v := range items {
		if v == day {
			return true
		}
	}
	return false
}

func toContractWeekday(wd time.Weekday) int {
	switch wd {
	case time.Monday:
		return 0
	case time.Tuesday:
		return 1
	case time.Wednesday:
		return 2
	case time.Thursday:
		return 3
	case time.Friday:
		return 4
	case time.Saturday:
		return 5
	default:
		return 6
	}
}

func parseDate(input string, loc *time.Location) (time.Time, bool) {
	if input == "" {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("2006-01-02", input, loc)
	if err != nil {
		return time.Time{}, false
	}
	return beginningOfDay(t), true
}

func toDateOnly(t time.Time) DateOnly {
	return DateOnly{Year: t.Year(), Month: t.Month(), Day: t.Day()}
}

func asString(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok
}

func asInt(v any) (int, bool) {
	switch t := v.(type) {
	case int:
		return t, true
	case int32:
		return int(t), true
	case int64:
		return int(t), true
	case float64:
		return int(t), true
	default:
		return 0, false
	}
}

func asIntDefault(v any, fallback int) int {
	if i, ok := asInt(v); ok {
		return i
	}
	return fallback
}

func asIntSlice(v any) ([]int, bool) {
	raw, ok := v.([]any)
	if !ok {
		if typed, ok := v.([]int); ok {
			return typed, true
		}
		return nil, false
	}
	out := make([]int, 0, len(raw))
	for _, item := range raw {
		val, ok := asInt(item)
		if !ok {
			return nil, false
		}
		out = append(out, val)
	}
	return out, true
}
