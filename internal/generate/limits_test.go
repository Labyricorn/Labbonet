package generate

import (
	"errors"
	"strings"
	"testing"
)

func feedAll(c *replyCap, deltas ...string) (string, bool) {
	var out strings.Builder
	for _, d := range deltas {
		keep, stop := c.feed(d)
		out.WriteString(keep)
		if stop {
			return out.String(), true
		}
	}
	return out.String(), false
}

func TestReplyCapStopsAtSentenceEnd(t *testing.T) {
	got, stopped := feedAll(&replyCap{limit: 10}, "Hello there", ". This is", " more. And more.")
	if !stopped || got != "Hello there." {
		t.Fatalf("got %q stopped=%v", got, stopped)
	}
	// A terminator at the very end of a delta is decided by the next delta.
	got, stopped = feedAll(&replyCap{limit: 5}, "Pi is 3.", "14 roughly. Next")
	if !stopped || got != "Pi is 3.14 roughly." {
		t.Fatalf("got %q stopped=%v", got, stopped)
	}
	got, stopped = feedAll(&replyCap{limit: 5}, "Done now.", " More")
	if !stopped || got != "Done now." {
		t.Fatalf("got %q stopped=%v", got, stopped)
	}
	got, stopped = feedAll(&replyCap{limit: 5}, `He said "stop." Then`)
	if !stopped || got != `He said "stop."` {
		t.Fatalf("got %q stopped=%v", got, stopped)
	}
	got, stopped = feedAll(&replyCap{limit: 5}, "line one\nline two")
	if !stopped || got != "line one\n" {
		t.Fatalf("got %q stopped=%v", got, stopped)
	}
}

func TestReplyCapHardStopAndDisabled(t *testing.T) {
	long := strings.Repeat("word ", 200)
	got, stopped := feedAll(&replyCap{limit: 100}, long)
	if !stopped || len([]rune(got)) != 100+replySlack {
		t.Fatalf("hard stop at %d runes, stopped=%v", len([]rune(got)), stopped)
	}
	got, stopped = feedAll(&replyCap{}, long)
	if stopped || got != long {
		t.Fatal("zero limit must not cap")
	}
	if got, stopped = feedAll(&replyCap{limit: 100}, "short."); stopped || got != "short." {
		t.Fatal("under the cap must pass through")
	}
}

func TestComposeUserContentFencesAndSanitizes(t *testing.T) {
	out, names, err := ComposeUserContent("look", []Attachment{{Name: `..\..\evil` + "`" + `x.txt`, Content: "a\n```\nb\n"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "evilx.txt" {
		t.Fatalf("names = %v", names)
	}
	if !strings.Contains(out, "[Attached file: evilx.txt]\n````\na\n```\nb\n````") {
		t.Fatalf("composed = %q", out)
	}
	for _, bad := range [][]Attachment{
		{{Name: "a", Content: "x\x00y"}},
		{{Name: "a", Content: "\xff\xfe"}},
		{{Name: "a", Content: strings.Repeat("x", MaxAttachmentBytes+1)}},
		make([]Attachment, MaxAttachments+1),
	} {
		if _, _, err := ComposeUserContent("t", bad); !errors.Is(err, ErrAttachment) {
			t.Fatalf("expected ErrAttachment for %.20v, got %v", bad[0].Name, err)
		}
	}
	if got := SafeAttachmentName(""); got != "attachment.txt" {
		t.Fatalf("empty name = %q", got)
	}
	if got := SafeAttachmentName(`C:\x\.hidden` + "\u202e" + `.txt`); got != "hidden.txt" {
		t.Fatalf("name = %q", got)
	}
}

func TestValidateLimits(t *testing.T) {
	if validateLimits(0, 0) != nil || validateLimits(500, 5000) != nil {
		t.Fatal("valid limits rejected")
	}
	if !errors.Is(validateLimits(5, 0), ErrLimits) || !errors.Is(validateLimits(0, 5), ErrLimits) {
		t.Fatal("out of range limits accepted")
	}
}
