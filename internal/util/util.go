package util

import (
	"database/sql"
	"fmt"
	"time"
)

var ValidDays = map[string]string{
	"mon": "Mon", "tue": "Tue", "wed": "Wed", "thu": "Thu",
	"fri": "Fri", "sat": "Sat", "sun": "Sun",
}

func ParseTimestamp(s string) (time.Time, error) {
	if t, err := time.Parse("2006-01-02 15:04:05", s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}

// FormatDuration renders a duration as "Xh YYm" or "Ym".
func FormatDuration(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %02dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

func NullOr(v sql.NullString) string {
	if v.Valid && v.String != "" {
		return v.String
	}
	return "-"
}

func MondayOf(t time.Time) time.Time {
	weekday := int(t.Weekday())
	if weekday == 0 { // Sunday
		weekday = 7
	}
	return t.AddDate(0, 0, -(weekday - 1))
}
