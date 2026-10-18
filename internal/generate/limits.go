package generate

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Reply and reasoning bounds (parity plan milestone 4). A zero limit disables the check.
const (
	MinReplyChars     = 100
	MaxReplyChars     = 200000
	MinReasoningChars = 500
	MaxReasoningChars = 1000000
	// replySlack is how far past the reply cap the stream may run to reach a sentence end.
	replySlack = 400
)

// Sentinel causes returned from the stream callback to end a generation early. They are
// not failures of the provider: a reply cap is a normal completion, the reasoning
// watchdog is recorded as an error with its own kind.
var (
	errReplyCap       = errors.New("reply length cap reached")
	errReasoningLimit = errors.New("reasoning limit reached")
)

// ErrLimits reports an out-of-range limit in a request.
var ErrLimits = errors.New("invalid reply limit")

func validateLimits(replyChars, reasoningChars int) error {
	if replyChars != 0 && (replyChars < MinReplyChars || replyChars > MaxReplyChars) {
		return fmt.Errorf("%w: reply cap must be %d to %d characters", ErrLimits, MinReplyChars, MaxReplyChars)
	}
	if reasoningChars != 0 && (reasoningChars < MinReasoningChars || reasoningChars > MaxReasoningChars) {
		return fmt.Errorf("%w: reasoning limit must be %d to %d characters", ErrLimits, MinReasoningChars, MaxReasoningChars)
	}
	return nil
}

// replyCap ends a reply at the first sentence end at or after limit characters. It is fed
// each content delta in order and returns the part of that delta to keep; stop reports
// that the reply is finished. If no sentence end appears within replySlack characters
// the reply is cut there.
type replyCap struct {
	limit   int
	count   int
	pending bool // the previous delta ended on a terminator; the boundary is decided by the next rune
}

func isTerminator(r rune) bool { return r == '.' || r == '!' || r == '?' || r == '…' }

func isCloser(r rune) bool { return strings.ContainsRune("\"'”’)]»", r) }

func (c *replyCap) feed(delta string) (keep string, stop bool) {
	if c.limit <= 0 || delta == "" {
		return delta, false
	}
	rs := []rune(delta)
	if c.pending {
		c.pending = false
		if unicode.IsSpace(rs[0]) {
			return "", true
		}
	}
	for i := 0; i < len(rs); i++ {
		c.count++
		r := rs[i]
		if c.count >= c.limit {
			if r == '\n' {
				return string(rs[:i+1]), true
			}
			if isTerminator(r) {
				j := i + 1
				for j < len(rs) && isCloser(rs[j]) {
					j++
				}
				if j < len(rs) {
					if unicode.IsSpace(rs[j]) {
						return string(rs[:j]), true
					}
				} else {
					c.pending = true
					c.count += j - i - 1
					break
				}
			}
		}
		if c.count >= c.limit+replySlack {
			return string(rs[:i+1]), true
		}
	}
	return delta, false
}

// Attachment limits. Files are read as text by the browser; the server re-validates
// everything because the API is reachable by any script holding the session.
const (
	MaxAttachments     = 5
	MaxAttachmentBytes = 256 << 10
	MaxAttachmentTotal = 512 << 10
	maxAttachmentName  = 100
)

// ErrAttachment reports a rejected attachment.
var ErrAttachment = errors.New("invalid attachment")

// Attachment is one text file attached to a user message.
type Attachment struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// SafeAttachmentName reduces a client-supplied file name to a short, printable base name.
func SafeAttachmentName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r == '`' || r == utf8.RuneError || unicode.IsControl(r) || unicode.Is(unicode.Cf, r):
		default:
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	out = strings.TrimLeft(out, ".")
	if r := []rune(out); len(r) > maxAttachmentName {
		out = string(r[:maxAttachmentName])
	}
	if out == "" {
		return "attachment.txt"
	}
	return out
}

// ComposeUserContent appends each attachment to the typed message as a labeled fenced
// block. The fence is longer than any backtick run inside the file so the content can
// never close it early. It returns the composed text and the stored file names.
func ComposeUserContent(text string, files []Attachment) (string, []string, error) {
	if len(files) == 0 {
		return text, nil, nil
	}
	if len(files) > MaxAttachments {
		return "", nil, fmt.Errorf("%w: at most %d files", ErrAttachment, MaxAttachments)
	}
	var b strings.Builder
	b.WriteString(text)
	names := make([]string, 0, len(files))
	total := 0
	for _, f := range files {
		if len(f.Content) > MaxAttachmentBytes {
			return "", nil, fmt.Errorf("%w: %q is larger than %d KiB", ErrAttachment, SafeAttachmentName(f.Name), MaxAttachmentBytes>>10)
		}
		total += len(f.Content)
		if total > MaxAttachmentTotal {
			return "", nil, fmt.Errorf("%w: attachments exceed %d KiB in total", ErrAttachment, MaxAttachmentTotal>>10)
		}
		if !utf8.ValidString(f.Content) || strings.ContainsRune(f.Content, 0) {
			return "", nil, fmt.Errorf("%w: %q is not text", ErrAttachment, SafeAttachmentName(f.Name))
		}
		name := SafeAttachmentName(f.Name)
		names = append(names, name)
		fence := strings.Repeat("`", max(3, longestBacktickRun(f.Content)+1))
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "[Attached file: %s]\n%s\n%s\n%s", name, fence, strings.TrimRight(f.Content, "\n"), fence)
	}
	return b.String(), names, nil
}

func longestBacktickRun(s string) int {
	longest, run := 0, 0
	for _, r := range s {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	return longest
}

// truncateUTF8 returns at most n bytes of s without splitting a character.
func truncateUTF8(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
