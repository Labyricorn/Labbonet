package interchange

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Labyricorn/Labbonet/internal/cast"
)

const (
	SnapshotFormat  = "labbonet-snapshot"
	SnapshotVersion = 2
	// minSnapshotVersion is the oldest readable version. Version 1 lists a single
	// linear branch per conversation; version 2 adds the message tree and organization.
	minSnapshotVersion = 1
	MaxUpload          = 64 << 20

	maxSnapCards    = 2000
	maxSnapConvs    = 5000
	maxSnapMessages = 500000
	maxMessageBytes = 1 << 20
)

// Snapshot is the versioned export/import document. It never contains endpoints,
// credentials, provider settings, models or generation parameters.
type Snapshot struct {
	Format        string             `json:"format"`
	Version       int                `json:"version"`
	Scope         string             `json:"scope"`
	ExportedAt    string             `json:"exported_at,omitempty"`
	Cards         []SnapCard         `json:"cards"`
	Conversations []SnapConversation `json:"conversations"`
	Defaults      *SnapDefaults      `json:"defaults,omitempty"`
}

type SnapCard struct {
	Key   string    `json:"key"`
	Card  cast.Card `json:"card"`
	Notes []string  `json:"-"`
}

type SnapDefaults struct {
	Character string `json:"character,omitempty"`
	Persona   string `json:"persona,omitempty"`
}

type SnapConversation struct {
	Key          string        `json:"key"`
	Title        string        `json:"title"`
	SystemPrompt string        `json:"system_prompt,omitempty"`
	CreatedAt    string        `json:"created_at,omitempty"`
	Character    string        `json:"character,omitempty"`
	Persona      string        `json:"persona,omitempty"`
	Greeting     int           `json:"greeting,omitempty"`
	Messages     []SnapMessage `json:"messages"`
	// Active names the tip of the visible branch (version 2).
	Active string   `json:"active,omitempty"`
	Pinned bool     `json:"pinned,omitempty"`
	Folder string   `json:"folder,omitempty"`
	Tags   []string `json:"tags,omitempty"`
}

type SnapMessage struct {
	Role      string         `json:"role"`
	Content   string         `json:"content"`
	Reasoning string         `json:"reasoning,omitempty"`
	Status    string         `json:"status"`
	CreatedAt string         `json:"created_at,omitempty"`
	Speaker   *cast.Identity `json:"speaker,omitempty"`
	// Tree fields (version 2). Key is unique within the conversation; Parent names an
	// earlier message; Selected names the child that continues this message's branch;
	// Origin/OriginKey record edit, reroll or fork provenance.
	Key       string `json:"key,omitempty"`
	Parent    string `json:"parent,omitempty"`
	Selected  string `json:"selected,omitempty"`
	Origin    string `json:"origin,omitempty"`
	OriginKey string `json:"origin_key,omitempty"`
}

// Validate checks the whole document before anything is written, so a rejected
// import cannot partially apply. It also normalizes imported identities.
func (s *Snapshot) Validate() error {
	if s.Format != SnapshotFormat {
		return errors.New("not a Labbonet snapshot")
	}
	if s.Version < minSnapshotVersion || s.Version > SnapshotVersion {
		return fmt.Errorf("unsupported snapshot version %d (this build reads versions %d to %d)", s.Version, minSnapshotVersion, SnapshotVersion)
	}
	if s.Scope != "full" && s.Scope != "conversation" && s.Scope != "cards" && s.Scope != "threads" {
		return errors.New("unknown snapshot scope")
	}
	if len(s.Cards) > maxSnapCards || len(s.Conversations) > maxSnapConvs {
		return errors.New("snapshot has too many cards or conversations")
	}
	kinds := map[string]string{}
	for i := range s.Cards {
		k := &s.Cards[i]
		if k.Key == "" || len(k.Key) > 64 {
			return fmt.Errorf("card %d has an invalid key", i+1)
		}
		if _, dup := kinds[k.Key]; dup {
			return fmt.Errorf("duplicate card key %q", k.Key)
		}
		k.Card.ID, k.Card.Revision, k.Card.Position = 0, 0, 0
		if err := k.Card.Validate(); err != nil {
			return fmt.Errorf("card %q: %w", truncate(k.Card.Name, 60), err)
		}
		kinds[k.Key] = k.Card.Kind
	}
	if d := s.Defaults; d != nil {
		if err := checkRef(kinds, d.Character, "character"); err != nil {
			return err
		}
		if err := checkRef(kinds, d.Persona, "persona"); err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	total := 0
	for i := range s.Conversations {
		c := &s.Conversations[i]
		if c.Key == "" || len(c.Key) > 64 || seen[c.Key] {
			return fmt.Errorf("conversation %d has a missing or duplicate key", i+1)
		}
		seen[c.Key] = true
		if strings.TrimSpace(c.Title) == "" || len(c.Title) > 500 || len(c.SystemPrompt) > 65536 {
			return fmt.Errorf("conversation %q has an invalid title or system prompt", truncate(c.Key, 20))
		}
		if err := checkRef(kinds, c.Character, "character"); err != nil {
			return err
		}
		if err := checkRef(kinds, c.Persona, "persona"); err != nil {
			return err
		}
		if c.Greeting < 0 || c.Greeting > 63 || (c.Character == "" && c.Greeting != 0) {
			return errors.New("invalid greeting selection")
		}
		if err := c.validateOrganization(); err != nil {
			return err
		}
		total += len(c.Messages)
		if total > maxSnapMessages {
			return errors.New("snapshot has too many messages")
		}
		for j := range c.Messages {
			m := &c.Messages[j]
			if m.Role != "user" && m.Role != "assistant" {
				return fmt.Errorf("conversation %q message %d has an invalid role", c.Key, j+1)
			}
			switch m.Status {
			case "complete", "cancelled", "error", "interrupted":
			case "streaming":
				m.Status = "interrupted"
			default:
				return fmt.Errorf("conversation %q message %d has an invalid status", c.Key, j+1)
			}
			if len(m.Content) > maxMessageBytes || len(m.Reasoning) > maxMessageBytes {
				return fmt.Errorf("conversation %q message %d is too large", c.Key, j+1)
			}
			if m.Speaker != nil {
				sp := *m.Speaker
				sp.ID, sp.Revision = 0, 0
				if len(sp.Name) > 200 || (sp.TextColor != "" && !hexColor(sp.TextColor)) || (sp.DialogColor != "" && !hexColor(sp.DialogColor)) {
					return fmt.Errorf("conversation %q message %d has an invalid speaker", c.Key, j+1)
				}
				if err := cast.ValidateImage(sp.Portrait); err != nil {
					return fmt.Errorf("conversation %q message %d speaker: %w", c.Key, j+1, err)
				}
				m.Speaker = &sp
			}
		}
		if err := c.normalizeTree(); err != nil {
			return err
		}
	}
	return nil
}

func hexColor(s string) bool {
	if len(s) != 7 || s[0] != '#' {
		return false
	}
	for _, r := range s[1:] {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
			return false
		}
	}
	return true
}

func checkRef(kinds map[string]string, key, kind string) error {
	if key == "" {
		return nil
	}
	if got, ok := kinds[key]; !ok || got != kind {
		return fmt.Errorf("reference %q does not name a %s in this snapshot", truncate(key, 20), kind)
	}
	return nil
}

// Item describes one parsed card for the import preview.
type Item struct {
	Name   string   `json:"name"`
	Kind   string   `json:"kind"`
	Format string   `json:"format"`
	Notes  []string `json:"notes"`
}

// Parsed is the validated content of an upload.
type Parsed struct {
	Snapshot *Snapshot
	Format   string
	Items    []Item
}

// Parse accepts a snapshot, native/Tavern card JSON, a Tavern PNG, or a CHARX/sharing ZIP.
func Parse(data []byte) (*Parsed, error) {
	if len(data) == 0 || len(data) > MaxUpload {
		return nil, errors.New("file is empty or larger than 64 MiB")
	}
	var p *parsedCard
	var err error
	switch {
	case bytes.HasPrefix(data, pngSignature):
		p, err = parsePNGCard(data)
	case bytes.HasPrefix(data, []byte("PK\x03\x04")):
		p, err = parseArchive(data)
	default:
		trimmed := bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
		if parsed, isGobbo, gerr := decodeGobboNet(trimmed); isGobbo {
			return parsed, gerr
		}
		if snap, isSnap, serr := decodeSnapshot(trimmed); isSnap {
			if serr != nil {
				return nil, serr
			}
			if err := snap.Validate(); err != nil {
				return nil, err
			}
			out := &Parsed{Snapshot: snap, Format: "Labbonet snapshot (" + snap.Scope + ")"}
			for _, c := range snap.Cards {
				out.Items = append(out.Items, Item{Name: c.Card.Name, Kind: c.Card.Kind, Format: "Labbonet", Notes: []string{}})
			}
			return out, nil
		}
		p, err = parseCardJSON(trimmed)
	}
	if err != nil {
		return nil, err
	}
	snap := &Snapshot{Format: SnapshotFormat, Version: SnapshotVersion, Scope: "cards", Cards: []SnapCard{{Key: "1", Card: p.Card}}}
	if err := snap.Validate(); err != nil {
		return nil, err
	}
	notes := p.Notes
	if notes == nil {
		notes = []string{}
	}
	return &Parsed{Snapshot: snap, Format: p.Format, Items: []Item{{Name: p.Card.Name, Kind: p.Card.Kind, Format: p.Format, Notes: notes}}}, nil
}

func decodeSnapshot(raw []byte) (*Snapshot, bool, error) {
	var probe struct {
		Format string `json:"format"`
	}
	if json.Unmarshal(raw, &probe) != nil || probe.Format != SnapshotFormat {
		return nil, false, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var s Snapshot
	if err := dec.Decode(&s); err != nil {
		return nil, true, fmt.Errorf("invalid snapshot: %w", err)
	}
	if dec.More() {
		return nil, true, errors.New("invalid snapshot: trailing data")
	}
	return &s, true, nil
}

const (
	maxSnapTags   = 16
	maxSnapTagLen = 32
	maxFolderLen  = 60
)

func (c *SnapConversation) validateOrganization() error {
	if len(c.Folder) > maxFolderLen*4 || len(c.Tags) > maxSnapTags {
		return fmt.Errorf("conversation %q has an invalid folder or too many tags", truncate(c.Key, 20))
	}
	for _, t := range c.Tags {
		if strings.TrimSpace(t) == "" || len([]rune(t)) > maxSnapTagLen {
			return fmt.Errorf("conversation %q has an invalid tag", truncate(c.Key, 20))
		}
	}
	return nil
}

// normalizeTree checks the message tree and, for linear (version 1) conversations,
// builds it: each message below the previous one, the last message active. After this
// returns, every message has a unique key and Parent/Selected/Active refer only to
// earlier or existing keys, so the importer needs a single code path.
func (c *SnapConversation) normalizeTree() error {
	keyed := 0
	for _, m := range c.Messages {
		if m.Key != "" {
			keyed++
		}
	}
	if keyed != 0 && keyed != len(c.Messages) {
		return fmt.Errorf("conversation %q mixes keyed and unkeyed messages", truncate(c.Key, 20))
	}
	if keyed == 0 {
		for i := range c.Messages {
			m := &c.Messages[i]
			m.Key = fmt.Sprintf("%d", i+1)
			if i > 0 {
				m.Parent = c.Messages[i-1].Key
				c.Messages[i-1].Selected = m.Key
			}
		}
		if n := len(c.Messages); n > 0 {
			c.Active = c.Messages[n-1].Key
		}
		return nil
	}
	index := map[string]int{}
	selectedBy := map[string]string{}
	for i := range c.Messages {
		m := &c.Messages[i]
		if len(m.Key) > 64 {
			return fmt.Errorf("conversation %q message %d has an invalid key", truncate(c.Key, 20), i+1)
		}
		if _, dup := index[m.Key]; dup {
			return fmt.Errorf("conversation %q has duplicate message key %q", truncate(c.Key, 20), truncate(m.Key, 20))
		}
		if m.Parent != "" {
			if p, ok := index[m.Parent]; !ok || p >= i {
				return fmt.Errorf("conversation %q message %d has a parent that does not precede it", truncate(c.Key, 20), i+1)
			}
		}
		switch m.Origin {
		case "", "original", "edit", "reroll", "fork":
		default:
			return fmt.Errorf("conversation %q message %d has an unknown origin", truncate(c.Key, 20), i+1)
		}
		if m.Origin == "" {
			m.Origin = "original"
		}
		if m.OriginKey != "" {
			if o, ok := index[m.OriginKey]; !ok || o >= i {
				m.OriginKey = "" // provenance link to a message outside the export is dropped
			}
		}
		index[m.Key] = i
	}
	for i := range c.Messages {
		m := &c.Messages[i]
		if m.Selected == "" {
			continue
		}
		child, ok := index[m.Selected]
		if !ok || c.Messages[child].Parent != m.Key {
			return fmt.Errorf("conversation %q message %d selects a message that is not its child", truncate(c.Key, 20), i+1)
		}
		if _, dup := selectedBy[m.Key]; dup {
			return errors.New("duplicate selection")
		}
		selectedBy[m.Key] = m.Selected
	}
	if c.Active == "" && len(c.Messages) > 0 {
		c.Active = c.Messages[len(c.Messages)-1].Key
	}
	if c.Active != "" {
		if _, ok := index[c.Active]; !ok {
			return fmt.Errorf("conversation %q names an unknown active message", truncate(c.Key, 20))
		}
	}
	return nil
}
