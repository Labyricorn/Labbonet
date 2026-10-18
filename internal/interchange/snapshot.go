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
	SnapshotVersion = 1
	MaxUpload       = 64 << 20

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
}

type SnapMessage struct {
	Role      string         `json:"role"`
	Content   string         `json:"content"`
	Reasoning string         `json:"reasoning,omitempty"`
	Status    string         `json:"status"`
	CreatedAt string         `json:"created_at,omitempty"`
	Speaker   *cast.Identity `json:"speaker,omitempty"`
}

// Validate checks the whole document before anything is written, so a rejected
// import cannot partially apply. It also normalizes imported identities.
func (s *Snapshot) Validate() error {
	if s.Format != SnapshotFormat {
		return errors.New("not a Labbonet snapshot")
	}
	if s.Version != SnapshotVersion {
		return fmt.Errorf("unsupported snapshot version %d (this build reads version %d)", s.Version, SnapshotVersion)
	}
	if s.Scope != "full" && s.Scope != "conversation" && s.Scope != "cards" {
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
