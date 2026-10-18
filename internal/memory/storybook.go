package memory

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Limits on authored storybooks. Everything here is inert text used for matching and
// optional prompt injection; none of it is interpreted as code.
const (
	MaxStorybookBytes = 256 << 10
	maxEntities       = 500
	maxTagsPerEntity  = 64
	maxProseChunks    = 2000
	maxEmbedBytes     = 4000
)

// Cost is the one context-budget estimate used by prompt assembly, summaries and
// retrieval: UTF-8 bytes plus framing. No tokenizer is verified for the registered
// providers, and bytes over-count real tokens, so budgets err on the safe side.
func Cost(text string) int { return len(text) + 16 }

// Tag is one weighted retrieval term from a tags field: "dragon" or "divine [0.8]".
type Tag struct {
	Term   string  `json:"term"`
	Weight float64 `json:"weight"`
}

// Entity is a storybook section (kind "entity") or a dotted subtree "parent.child".
type Entity struct {
	ID               string   `json:"id"`
	Kind             string   `json:"kind"`
	Parent           string   `json:"parent,omitempty"`
	Tags             []Tag    `json:"tags"`
	Relations        []string `json:"relations,omitempty"`
	RoleInArcs       string   `json:"role_in_arcs,omitempty"`
	UsePrinciples    string   `json:"use,omitempty"`
	OffscreenDefault bool     `json:"offscreen_default,omitempty"`
	Prose            string   `json:"prose,omitempty"`
}

// ProseChunk is free prose outside any entity section; it is reachable only by
// semantic retrieval.
type ProseChunk struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// Counts summarize a parsed storybook for the editor preview.
type Counts struct {
	Entities    int `json:"entities"`
	Subtrees    int `json:"subtrees"`
	Tags        int `json:"tags"`
	ProseChunks int `json:"prose_chunks"`
}

// Book is a parsed storybook.
type Book struct {
	Entities    map[string]*Entity `json:"-"`
	Order       []string           `json:"order"`
	ProseChunks []ProseChunk       `json:"prose_chunks,omitempty"`
	Counts      Counts             `json:"counts"`
	// Warnings are authoring problems (dangling relations, entities with nothing to
	// inject). Parsing never fails on them; unknown lines stay prose.
	Warnings []string `json:"warnings,omitempty"`
}

// Empty reports whether nothing retrievable was authored.
func (b *Book) Empty() bool { return b == nil || (len(b.Order) == 0 && len(b.ProseChunks) == 0) }

var (
	directiveRe     = regexp.MustCompile(`(?i)^([A-Za-z0-9][A-Za-z0-9_.\- ]*?)_(tags|relation(?:_[A-Za-z0-9]+)?|role_in_arcs|use(?:_[A-Za-z0-9]+)?|offscreen_default)\s*:\s*(.*)$`)
	bareDirectiveRe = regexp.MustCompile(`(?i)^(tags|relation(?:_[A-Za-z0-9]+)?|role_in_arcs|use(?:_[A-Za-z0-9]+)?|offscreen_default)\s*:\s*(.*)$`)
	headerRe        = regexp.MustCompile(`^(?:#{1,3}\s*|==\s*|\[)\s*([A-Za-z0-9][A-Za-z0-9_.\- ]*?)\s*(?:==|\])?\s*$`)
	ruleRe          = regexp.MustCompile(`^\s*([-=_*])\s*(?:[-=_*]\s*){2,}$`)
	weightRe        = regexp.MustCompile(`^(.*?)\s*\[\s*([0-9.]+)\s*\]\s*$`)
	truthyRe        = regexp.MustCompile(`(?i)^(1|true|yes|on)$`)
)

// ParseTags parses one tags value. Bare terms weigh 1.0; a bracketed number tunes the
// term. The bracket grammar lives only in tag fields and is never sent to the model.
func ParseTags(value string) []Tag {
	var out []Tag
	for _, raw := range strings.Split(value, ",") {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		if m := weightRe.FindStringSubmatch(s); m != nil {
			term := strings.TrimSpace(m[1])
			w, err := strconv.ParseFloat(m[2], 64)
			if term == "" {
				continue
			}
			if err != nil || !finite(w) || w < 0 || w > 100 {
				w = 1
			}
			out = append(out, Tag{Term: term, Weight: w})
			continue
		}
		out = append(out, Tag{Term: s, Weight: 1})
	}
	return out
}

// ParseStorybook supports the flat form ("selah_tags: angel [1.0]") and the sectioned
// form ("# selah" then bare "tags: ..." and prose). Recognized directives become
// control data (tags, relations, use principles); other lines are prose attached to the
// current section or, outside any section, grouped into standalone prose chunks.
func ParseStorybook(raw string) (*Book, error) {
	if len(raw) > MaxStorybookBytes {
		return nil, fmt.Errorf("storybook exceeds %d KiB", MaxStorybookBytes>>10)
	}
	book := &Book{Entities: map[string]*Entity{}}
	if strings.TrimSpace(raw) == "" {
		return book, nil
	}
	var limitErr error
	ensure := func(id string) *Entity {
		if e := book.Entities[id]; e != nil {
			return e
		}
		if len(book.Order) >= maxEntities {
			limitErr = fmt.Errorf("storybook has more than %d entities", maxEntities)
			return nil
		}
		e := &Entity{ID: id, Kind: "entity"}
		if i := strings.Index(id, "."); i > 0 {
			e.Kind, e.Parent = "subtree", id[:i]
		}
		book.Entities[id] = e
		book.Order = append(book.Order, id)
		return e
	}
	apply := func(e *Entity, suffix, value string) {
		switch s := strings.ToLower(suffix); {
		case s == "tags":
			e.Tags = append(e.Tags, ParseTags(value)...)
			if len(e.Tags) > maxTagsPerEntity {
				e.Tags = e.Tags[:maxTagsPerEntity]
				book.Warnings = append(book.Warnings, fmt.Sprintf("%s: more than %d tags; extra tags ignored", e.ID, maxTagsPerEntity))
			}
		case s == "role_in_arcs":
			e.RoleInArcs = strings.TrimSpace(value)
		case s == "offscreen_default":
			e.OffscreenDefault = truthyRe.MatchString(strings.TrimSpace(value))
		case strings.HasPrefix(s, "relation"):
			for _, r := range strings.Split(value, ",") {
				if r = strings.TrimSpace(r); r != "" {
					e.Relations = append(e.Relations, r)
				}
			}
		case strings.HasPrefix(s, "use"):
			if e.UsePrinciples != "" {
				e.UsePrinciples += " "
			}
			e.UsePrinciples += strings.TrimSpace(value)
		}
	}
	var current *Entity
	var buf []string
	flush := func() {
		text := strings.TrimSpace(strings.Join(buf, "\n"))
		buf = nil
		if text == "" {
			return
		}
		if len(book.ProseChunks) >= maxProseChunks {
			limitErr = fmt.Errorf("storybook has more than %d prose chunks", maxProseChunks)
			return
		}
		book.ProseChunks = append(book.ProseChunks, ProseChunk{ID: "doc:" + strconv.Itoa(len(book.ProseChunks)), Text: text})
	}
	for _, rawLine := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		line := strings.TrimRightFunc(rawLine, unicode.IsSpace)
		if strings.TrimSpace(line) == "" {
			if current == nil {
				flush()
			}
			continue
		}
		// A horizontal rule closes the section so following prose is standalone again.
		if ruleRe.MatchString(line) {
			if current == nil {
				flush()
			}
			current = nil
			continue
		}
		if m := directiveRe.FindStringSubmatch(line); m != nil {
			if e := ensure(strings.TrimSpace(m[1])); e != nil {
				apply(e, m[2], m[3])
				current = e
			}
			continue
		}
		if current != nil {
			if m := bareDirectiveRe.FindStringSubmatch(line); m != nil {
				apply(current, m[1], m[2])
				continue
			}
		}
		if m := headerRe.FindStringSubmatch(line); m != nil && !strings.Contains(line, ":") {
			if current == nil {
				flush()
			}
			current = ensure(strings.TrimSpace(m[1]))
			continue
		}
		if current != nil {
			if current.Prose != "" {
				current.Prose += "\n"
			}
			current.Prose += line
		} else {
			buf = append(buf, line)
		}
		if limitErr != nil {
			return nil, limitErr
		}
	}
	flush()
	if limitErr != nil {
		return nil, limitErr
	}
	for _, id := range book.Order {
		e := book.Entities[id]
		if e.Kind == "subtree" {
			book.Counts.Subtrees++
		} else {
			book.Counts.Entities++
		}
		book.Counts.Tags += len(e.Tags)
		if e.Prose != "" {
			book.Counts.ProseChunks++
		}
		if e.Prose == "" && e.UsePrinciples == "" {
			book.Warnings = append(book.Warnings, id+": no description text, so it can never be injected")
		}
		if len(e.Tags) == 0 {
			book.Warnings = append(book.Warnings, id+": no tags, so only semantic or relational retrieval can reach it")
		}
		for _, rel := range e.Relations {
			if book.resolve(rel) == "" {
				book.Warnings = append(book.Warnings, fmt.Sprintf("%s: relation %q matches no entity", id, rel))
			}
		}
	}
	book.Counts.ProseChunks += len(book.ProseChunks)
	sort.Strings(book.Warnings)
	if len(book.Warnings) > 50 {
		book.Warnings = append(book.Warnings[:50], fmt.Sprintf("... %d more", len(book.Warnings)-50))
	}
	return book, nil
}

// resolve maps a relation name to an entity id: exact id, else a unique entity whose
// last dotted segment matches (case-insensitively). "" = no match.
func (b *Book) resolve(name string) string {
	if _, ok := b.Entities[name]; ok {
		return name
	}
	found := ""
	for _, id := range b.Order {
		leaf := id[strings.LastIndex(id, ".")+1:]
		if strings.EqualFold(leaf, name) || strings.EqualFold(id, name) {
			if found != "" {
				return ""
			}
			found = id
		}
	}
	return found
}

// Doc is one embeddable text: an entity description or a standalone prose chunk.
type Doc struct {
	ID       string // entity id or "doc:N"; the embedding cache and records use it
	Kind     string // "entity", "subtree" or "doc"
	EntityID string // empty for standalone prose
	Text     string
}

// Docs lists everything semantic retrieval can embed, in a stable order. Text is cut at
// a rune boundary so one chunk can never become an oversized provider request.
func (b *Book) Docs() []Doc {
	var out []Doc
	for _, id := range b.Order {
		e := b.Entities[id]
		if strings.TrimSpace(e.Prose) != "" {
			out = append(out, Doc{ID: id, Kind: e.Kind, EntityID: id, Text: clip(e.Prose, maxEmbedBytes)})
		}
	}
	for _, c := range b.ProseChunks {
		out = append(out, Doc{ID: c.ID, Kind: "doc", Text: clip(c.Text, maxEmbedBytes)})
	}
	return out
}

func clip(s string, max int) string {
	if len(s) <= max {
		return s
	}
	s = s[:max]
	for !utf8.ValidString(s) && len(s) > 0 {
		s = s[:len(s)-1]
	}
	return s
}
