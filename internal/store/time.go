package store

import (
	"fmt"
	"time"
)

// timeFormat is the on-disk representation for all timestamp columns:
// RFC3339 with nanosecond precision, always UTC.
const timeFormat = time.RFC3339Nano

func formatTime(t time.Time) string {
	return t.UTC().Format(timeFormat)
}

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(timeFormat, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse timestamp %q: %w", s, err)
	}
	return t, nil
}
