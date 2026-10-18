// Package schedule holds the pure rules for scheduled prompts: validation and the next
// run time for one-off and daily schedules in an IANA time zone. It has no database or
// network access, so every decision is testable with fixed clocks.
// Original implementation: Copyright (C) 2026 Labbonet contributors. AGPL-3.0-or-later.
// Behavior reference (thread, time and prompt per schedule; one-off and daily kinds;
// enable/duplicate; status): GobboNet js/22-scheduler.js @
// 314bdc9cae204ae38fdc1002ac6a9fb1437c109f, Copyright (C) 2026 Elodine / GoblinCorps. No
// upstream source copied. Exact attribution boundaries: docs/REUSE-LOG.md.
package schedule

import (
	"errors"
	"fmt"
	"strings"
	"time"
	_ "time/tzdata" // zone data travels with the binary; Windows has no system zoneinfo
	"unicode/utf8"
)

// Limits. Unattended prompts are deliberately small and bounded.
const (
	MaxSchedules    = 50
	MaxNameRunes    = 80
	MaxPromptBytes  = 8 << 10
	MaxAutoContinue = 5
	// ContinuePrompt is the fixed text of an automatic follow-up turn; it is not configurable
	// so a schedule can never smuggle a second arbitrary prompt into an unattended chain.
	ContinuePrompt = "Continue."
)

// Kinds.
const (
	KindOnce  = "once"
	KindDaily = "daily"
)

var errInvalid = errors.New("invalid schedule")

// IsInvalid reports whether err came from Validate.
func IsInvalid(err error) bool { return errors.Is(err, errInvalid) }

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errInvalid, fmt.Sprintf(format, args...))
}

// Schedule is one scheduled prompt. Date is used by "once" only ("2006-01-02"); Time is the
// local wall-clock time ("15:04") in Timezone. NextRun, RunCount and Last* are derived.
type Schedule struct {
	ID             int64  `json:"id"`
	Revision       int64  `json:"revision"`
	Name           string `json:"name"`
	ConversationID *int64 `json:"conversation_id"`
	Prompt         string `json:"prompt"`
	Kind           string `json:"kind"`
	Timezone       string `json:"timezone"`
	Date           string `json:"date,omitempty"`
	Time           string `json:"time"`
	Enabled        bool   `json:"enabled"`
	WebSearch      bool   `json:"web_search"`
	AutoContinue   int    `json:"auto_continue"`

	NextRun    *time.Time `json:"next_run"`
	RunCount   int        `json:"run_count"`
	LastStatus string     `json:"last_status,omitempty"`
	LastRunAt  *time.Time `json:"last_run_at,omitempty"`
}

// Location resolves the schedule's zone ("Local" is refused so a stored schedule means the
// same instant on every machine).
func (s Schedule) Location() (*time.Location, error) {
	name := strings.TrimSpace(s.Timezone)
	if name == "" || strings.EqualFold(name, "local") {
		return nil, invalid("choose a time zone such as America/Los_Angeles or UTC")
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, invalid("unknown time zone %q", name)
	}
	return loc, nil
}

func parseClock(s string) (h, m int, err error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, 0, invalid("time must look like 07:30")
	}
	return t.Hour(), t.Minute(), nil
}

// Validate checks the editable fields and normalises them in place.
func (s *Schedule) Validate() error {
	s.Name = strings.TrimSpace(s.Name)
	s.Prompt = strings.TrimSpace(s.Prompt)
	s.Kind = strings.TrimSpace(s.Kind)
	s.Time = strings.TrimSpace(s.Time)
	s.Date = strings.TrimSpace(s.Date)
	switch {
	case s.Name == "" || utf8.RuneCountInString(s.Name) > MaxNameRunes:
		return invalid("name must be 1 to %d characters", MaxNameRunes)
	case s.Prompt == "":
		return invalid("the prompt cannot be empty")
	case len(s.Prompt) > MaxPromptBytes:
		return invalid("the prompt is too long (limit %d KiB)", MaxPromptBytes>>10)
	case !utf8.ValidString(s.Prompt) || strings.ContainsRune(s.Prompt, 0):
		return invalid("the prompt must be plain text")
	case s.ConversationID == nil || *s.ConversationID <= 0:
		return invalid("choose a thread")
	case s.AutoContinue < 0 || s.AutoContinue > MaxAutoContinue:
		return invalid("auto-continue must be 0 to %d", MaxAutoContinue)
	}
	if _, err := s.Location(); err != nil {
		return err
	}
	if _, _, err := parseClock(s.Time); err != nil {
		return err
	}
	switch s.Kind {
	case KindDaily:
		s.Date = ""
	case KindOnce:
		if _, err := time.Parse("2006-01-02", s.Date); err != nil {
			return invalid("a one-off schedule needs a date like 2026-12-31")
		}
	default:
		return invalid("kind must be %q or %q", KindOnce, KindDaily)
	}
	return nil
}

// Next returns the first run strictly after the given instant. A one-off schedule has none
// once its time has passed. A daily schedule keeps its local wall-clock time across daylight
// saving changes: a time that does not exist that day runs at the first valid moment after
// it, and a time that happens twice runs once (the first occurrence).
func (s Schedule) Next(after time.Time) (time.Time, bool) {
	loc, err := s.Location()
	if err != nil {
		return time.Time{}, false
	}
	h, m, err := parseClock(s.Time)
	if err != nil {
		return time.Time{}, false
	}
	switch s.Kind {
	case KindOnce:
		d, err := time.ParseInLocation("2006-01-02", s.Date, loc)
		if err != nil {
			return time.Time{}, false
		}
		t, ok := wallTime(d.Year(), d.Month(), d.Day(), h, m, loc)
		return t, ok && t.After(after)
	case KindDaily:
		local := after.In(loc)
		// Start the day before: a wall time on the previous local date can still be later
		// than after across a zone change. Bounded by a few iterations.
		for i := -1; i < 4; i++ {
			day := time.Date(local.Year(), local.Month(), local.Day()+i, 0, 0, 0, 0, time.UTC)
			t, ok := wallTime(day.Year(), day.Month(), day.Day(), h, m, loc)
			if ok && t.After(after) {
				return t, true
			}
		}
	}
	return time.Time{}, false
}

// Resolve explicitly instead of relying on time.Date's unspecified choice around
// timezone transitions. UTC traversal chooses the first repeated minute, or the
// first valid minute after a gap. A wholly skipped calendar date has no run.
func wallTime(year int, month time.Month, day, hour, minute int, loc *time.Location) (time.Time, bool) {
	start := time.Date(year, month, day, 0, 0, 0, 0, time.UTC).Add(-24 * time.Hour)
	for i := 0; i < 72*60; i++ {
		instant := start.Add(time.Duration(i) * time.Minute)
		local := instant.In(loc)
		if local.Year() == year && local.Month() == month && local.Day() == day && local.Hour()*60+local.Minute() >= hour*60+minute {
			return instant, true
		}
	}
	return time.Time{}, false
}
