package schedule

import (
	"strings"
	"testing"
	"time"
)

func base() Schedule {
	id := int64(3)
	return Schedule{Name: "Morning", ConversationID: &id, Prompt: "Summarise the news", Kind: KindDaily, Timezone: "America/Los_Angeles", Time: "07:30"}
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestValidateRejectsBadInput(t *testing.T) {
	cases := map[string]func(*Schedule){
		"empty name":      func(s *Schedule) { s.Name = "  " },
		"long name":       func(s *Schedule) { s.Name = strings.Repeat("n", MaxNameRunes+1) },
		"empty prompt":    func(s *Schedule) { s.Prompt = "" },
		"huge prompt":     func(s *Schedule) { s.Prompt = strings.Repeat("p", MaxPromptBytes+1) },
		"nul prompt":      func(s *Schedule) { s.Prompt = "a\x00b" },
		"no thread":       func(s *Schedule) { s.ConversationID = nil },
		"local zone":      func(s *Schedule) { s.Timezone = "Local" },
		"unknown zone":    func(s *Schedule) { s.Timezone = "Mars/Olympus" },
		"empty zone":      func(s *Schedule) { s.Timezone = "" },
		"bad clock":       func(s *Schedule) { s.Time = "25:99" },
		"bad kind":        func(s *Schedule) { s.Kind = "weekly" },
		"once no date":    func(s *Schedule) { s.Kind = KindOnce },
		"once bad date":   func(s *Schedule) { s.Kind = KindOnce; s.Date = "tomorrow" },
		"too many chains": func(s *Schedule) { s.AutoContinue = MaxAutoContinue + 1 },
		"negative chain":  func(s *Schedule) { s.AutoContinue = -1 },
	}
	for name, mutate := range cases {
		s := base()
		mutate(&s)
		if err := s.Validate(); err == nil || !IsInvalid(err) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	ok := base()
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid schedule refused: %v", err)
	}
}

func TestDailyNextKeepsWallClockAcrossDST(t *testing.T) {
	s := base()
	s.Time = "07:30"
	// 2026-03-07 15:30Z is 07:30 PST; the next run is 07:30 PDT the day after the change.
	next, ok := s.Next(mustTime(t, "2026-03-07T15:30:00Z"))
	if !ok || !next.Equal(mustTime(t, "2026-03-08T14:30:00Z")) {
		t.Fatalf("next = %v %v", next, ok)
	}
	next, ok = s.Next(mustTime(t, "2026-03-08T14:30:00Z"))
	if !ok || !next.Equal(mustTime(t, "2026-03-09T14:30:00Z")) {
		t.Fatalf("after the change next = %v %v", next, ok)
	}
}

func TestDailyNonexistentTimeRunsOncePerDay(t *testing.T) {
	s := base()
	s.Time = "02:30" // does not exist on 2026-03-08 in Los Angeles
	next, ok := s.Next(mustTime(t, "2026-03-08T08:00:00Z"))
	if !ok || !next.Equal(mustTime(t, "2026-03-08T10:00:00Z")) {
		t.Fatalf("gap must advance to 03:00 PDT, got %v", next)
	}
	seen := map[string]int{}
	cur := mustTime(t, "2026-03-07T12:00:00Z")
	for i := 0; i < 4; i++ {
		next, ok := s.Next(cur)
		if !ok {
			t.Fatal("no next run")
		}
		if !next.After(cur) {
			t.Fatalf("next %v is not after %v", next, cur)
		}
		loc, _ := s.Location()
		seen[next.In(loc).Format("2006-01-02")]++
		cur = next
	}
	for day, n := range seen {
		if n != 1 {
			t.Errorf("%s ran %d times", day, n)
		}
	}
	if len(seen) != 4 {
		t.Errorf("days = %v", seen)
	}
}

func TestDailyRepeatedHourRunsOnce(t *testing.T) {
	s := base()
	s.Time = "01:30" // 01:30 happens twice on 2026-11-01 in Los Angeles
	next, ok := s.Next(mustTime(t, "2026-11-01T07:00:00Z"))
	if !ok || !next.Equal(mustTime(t, "2026-11-01T08:30:00Z")) {
		t.Fatalf("repeat must choose first 01:30 PDT, got %v", next)
	}
	cur := mustTime(t, "2026-10-31T12:00:00Z")
	count := 0
	for i := 0; i < 3; i++ {
		next, ok := s.Next(cur)
		if !ok {
			t.Fatal("no next")
		}
		if next.In(time.UTC).Format("2006-01-02") == "2026-11-01" {
			count++
		}
		cur = next
	}
	if count != 1 {
		t.Fatalf("runs on the repeated day = %d", count)
	}
}

func TestOnceFiresOnlyInFuture(t *testing.T) {
	s := base()
	s.Kind, s.Date, s.Time = KindOnce, "2026-12-31", "23:00"
	next, ok := s.Next(mustTime(t, "2026-12-01T00:00:00Z"))
	if !ok || !next.Equal(mustTime(t, "2027-01-01T07:00:00Z")) {
		t.Fatalf("next = %v %v", next, ok)
	}
	if _, ok := s.Next(mustTime(t, "2027-01-01T07:00:00Z")); ok {
		t.Fatal("a one-off schedule has a next run after it fired")
	}
}

func TestNextAcrossUTCDateBoundary(t *testing.T) {
	s := base()
	s.Timezone, s.Time = "Pacific/Auckland", "09:00"
	next, ok := s.Next(mustTime(t, "2026-06-01T20:59:00Z")) // 08:59 NZST on 2 June
	if !ok || !next.Equal(mustTime(t, "2026-06-01T21:00:00Z")) {
		t.Fatalf("next = %v %v", next, ok)
	}
	next, _ = s.Next(next)
	if !next.Equal(mustTime(t, "2026-06-02T21:00:00Z")) {
		t.Fatalf("following next = %v", next)
	}
}
