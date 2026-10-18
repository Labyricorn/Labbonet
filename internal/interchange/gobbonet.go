// Original implementation: Copyright (C) 2026 Labbonet contributors. AGPL-3.0-or-later.
// Data shape reference: GobboNet js/04-state.js, 07-prompt.js, 10-chat.js,
// 17-personas.js and 21-data.js @ 314bdc9cae204ae38fdc1002ac6a9fb1437c109f,
// Copyright (C) 2026 Elodine / GoblinCorps. No upstream runtime copied.
package interchange

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Labyricorn/Labbonet/internal/cast"
)

// Only the pinned export envelope is accepted. Settings, credentials, extensions,
// endpoint/model selections and schedules cannot activate capabilities on import.
func decodeGobboNet(data []byte) (*Parsed, bool, error) {
	var e struct {
		Export     string                      `json:"gobbonet_export"`
		Version    int                         `json:"version"`
		Schema     int                         `json:"schemaVersion"`
		Characters []json.RawMessage           `json:"characterCards"`
		Personas   []json.RawMessage           `json:"personaCards"`
		Threads    []gobboThread               `json:"threads"`
		Folders    []struct{ ID, Name string } `json:"folders"`
		Character  string                      `json:"activeCardId"`
		Persona    string                      `json:"activePersonaId"`
	}
	var probe map[string]json.RawMessage
	if json.Unmarshal(data, &probe) != nil || probe["gobbonet_export"] == nil {
		return nil, false, nil
	}
	if json.Unmarshal(data, &e) != nil {
		return nil, true, errors.New("malformed GobboNet export")
	}
	if e.Version != 1 || e.Schema < 0 || e.Schema > 1 {
		return nil, true, errors.New("unsupported GobboNet export or state schema version")
	}
	switch e.Export {
	case "full", "threads", "cards", "personas":
	default:
		return nil, true, errors.New("unknown GobboNet export scope")
	}
	if len(e.Characters)+len(e.Personas) > maxSnapCards || len(e.Threads) > maxSnapConvs {
		return nil, true, errors.New("too many GobboNet cards or threads")
	}
	s := &Snapshot{Format: SnapshotFormat, Version: SnapshotVersion, Scope: "full", Cards: []SnapCard{}, Conversations: []SnapConversation{}}
	out := &Parsed{Snapshot: s, Format: "GobboNet export (" + e.Export + ")", Items: []Item{}}
	refs := map[string]string{}
	identities := map[string]cast.Identity{}
	for _, group := range []struct {
		kind  string
		cards []json.RawMessage
	}{{"character", e.Characters}, {"persona", e.Personas}} {
		for _, raw := range group.cards {
			c, id, notes, err := gobboCard(raw, group.kind)
			if err != nil {
				return nil, true, err
			}
			ref := group.kind + ":" + id
			if id == "" || refs[ref] != "" {
				return nil, true, errors.New("missing or duplicate GobboNet card id")
			}
			key := fmt.Sprintf("c%d", len(s.Cards)+1)
			refs[ref] = key
			identities[ref] = cast.Identity{Name: c.Name, Portrait: c.Portrait, TextColor: c.TextColor, DialogColor: c.DialogColor}
			s.Cards = append(s.Cards, SnapCard{Key: key, Card: c})
			out.Items = append(out.Items, Item{Name: c.Name, Kind: c.Kind, Format: "GobboNet", Notes: notes})
		}
	}
	folders := map[string]string{}
	for _, f := range e.Folders {
		folders[f.ID] = f.Name
	}
	seen := map[string]bool{}
	total, expandedBytes := 0, 0
	for _, t := range e.Threads {
		if t.ID == "" || seen[t.ID] {
			return nil, true, errors.New("missing or duplicate GobboNet thread id")
		}
		seen[t.ID] = true
		title := t.Title
		if title == "" {
			title = t.Name
		}
		if strings.TrimSpace(title) == "" {
			title = "Imported thread"
		}
		c := SnapConversation{Key: fmt.Sprintf("t%d", len(s.Conversations)+1), Title: title, Character: refs["character:"+t.CardID], Persona: refs["persona:"+t.PersonaID], Pinned: t.Pinned, Folder: folders[t.FolderID], Tags: t.Tags, Messages: []SnapMessage{}}
		b := gobboTree{conv: &c, identities: identities, character: t.CardID, persona: t.PersonaID, total: &total, expandedBytes: &expandedBytes, index: map[string]int{}}
		leaf, err := b.sequence(t.Messages, "", 0)
		if err != nil {
			return nil, true, err
		}
		c.Active = leaf
		s.Conversations = append(s.Conversations, c)
	}
	s.Defaults = &SnapDefaults{Character: refs["character:"+e.Character], Persona: refs["persona:"+e.Persona]}
	out.Items = append(out.Items, Item{Name: "Compatibility report", Kind: "import", Format: "GobboNet", Notes: []string{
		"Cards, variants and continuations, bundled cast identities, folders, pins and tags are mapped. Missing cast uses Assistant/You.",
		"Provider/model selections, credentials, settings, search data, attachments, summaries, schedules and extensions are not imported or activated. Choose a registered model before sending.",
		"Timestamps and manual order are not carried over. Assistant rerolls share a copied continuation; user variants keep their own continuation.",
	}})
	if err := s.Validate(); err != nil {
		return nil, true, fmt.Errorf("invalid GobboNet content: %w", err)
	}
	return out, true, nil
}

type gobboThread struct {
	Name      string         `json:"name"`
	ID        string         `json:"id"`
	Title     string         `json:"title"`
	CardID    string         `json:"cardId"`
	PersonaID string         `json:"personaId"`
	FolderID  string         `json:"folderId"`
	Pinned    bool           `json:"pinned"`
	Tags      []string       `json:"tags"`
	Messages  []gobboMessage `json:"messages"`
}
type gobboMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	Reasoning  string         `json:"reasoning"`
	CardID     string         `json:"cardId"`
	PersonaID  string         `json:"personaId"`
	Variants   []gobboVariant `json:"variants"`
	Active     int            `json:"activeVariant"`
	Generating bool           `json:"generating"`
}
type gobboVariant struct {
	Content      string         `json:"content"`
	Reasoning    string         `json:"reasoning"`
	Continuation []gobboMessage `json:"continuation"`
}
type gobboTree struct {
	conv               *SnapConversation
	identities         map[string]cast.Identity
	character, persona string
	total              *int
	expandedBytes      *int
	index              map[string]int
}

func (b *gobboTree) sequence(messages []gobboMessage, parent string, depth int) (string, error) {
	if depth > 64 {
		return "", errors.New("GobboNet branch nesting exceeds 64")
	}
	for pos, m := range messages {
		if m.Role != "user" && m.Role != "assistant" {
			return "", errors.New("invalid GobboNet message role")
		}
		variants := m.Variants
		active := m.Active
		if len(variants) == 0 {
			variants = []gobboVariant{{Content: m.Content, Reasoning: m.Reasoning}}
			active = 0
		}
		if len(variants) > 64 || active < 0 || active >= len(variants) {
			return "", errors.New("invalid GobboNet message variants")
		}
		selected := ""
		leaf := ""
		for i, v := range variants {
			(*b.total)++
			if *b.total > maxSnapMessages {
				return "", errors.New("too many expanded GobboNet messages")
			}
			content, reasoning := v.Content, v.Reasoning
			if i == active {
				content, reasoning = m.Content, m.Reasoning
			}
			*b.expandedBytes += len(content) + len(reasoning)
			if *b.expandedBytes > MaxUpload {
				return "", errors.New("expanded GobboNet branches exceed 64 MiB")
			}
			key := fmt.Sprintf("m%d", len(b.conv.Messages)+1)
			status := "complete"
			if m.Generating && i == active {
				status = "interrupted"
			}
			node := SnapMessage{Key: key, Parent: parent, Role: m.Role, Content: content, Reasoning: reasoning, Status: status}
			ref := "character:" + m.CardID
			if m.CardID == "" {
				ref = "character:" + b.character
			}
			if m.Role == "user" {
				ref = "persona:" + m.PersonaID
				if m.PersonaID == "" {
					ref = "persona:" + b.persona
				}
			}
			if id, ok := b.identities[ref]; ok {
				copy := id
				node.Speaker = &copy
			}
			b.index[key] = len(b.conv.Messages)
			b.conv.Messages = append(b.conv.Messages, node)
			if i == active {
				selected = key
				if parent != "" {
					b.conv.Messages[b.index[parent]].Selected = key
				}
			}
			if len(variants) > 1 {
				downstream := messages[pos+1:]
				if m.Role == "user" && i != active {
					downstream = v.Continuation
				}
				end, err := b.sequence(downstream, key, depth+1)
				if err != nil {
					return "", err
				}
				if i == active {
					leaf = end
				}
			}
		}
		if len(variants) > 1 {
			return leaf, nil
		}
		parent = selected
	}
	return parent, nil
}

func gobboCard(raw []byte, kind string) (cast.Card, string, []string, error) {
	var in struct {
		ID, Name, Description, Personality                                                                                                          string
		Avatar, Background, WritingStyle, StartingLore, RagStorybook, Greeting, AltGreetings, CarouselPrompts, CarouselMode, TextColor, DialogColor string
		AltGreetingsEnabled, CarouselEnabled                                                                                                        bool
		InjectionFrequency                                                                                                                          *int
		ContextLimit                                                                                                                                int
		Temperature, TopP                                                                                                                           *float64
	}
	if json.Unmarshal(raw, &in) != nil {
		return cast.Card{}, "", nil, errors.New("malformed GobboNet card")
	}
	c := cast.Card{Kind: kind, Name: in.Name, Description: in.Description, Personality: in.Personality, Style: in.WritingStyle, Lore: in.StartingLore, Storybook: in.RagStorybook, TextColor: in.TextColor, DialogColor: in.DialogColor, ContextLimit: in.ContextLimit, Temperature: in.Temperature, TopP: in.TopP, PersonaFrequency: 5}
	if in.InjectionFrequency != nil {
		c.PersonaFrequency = *in.InjectionFrequency
	}
	if in.Greeting != "" {
		c.Greetings = append(c.Greetings, in.Greeting)
	}
	if in.AltGreetingsEnabled {
		for _, g := range strings.Split(strings.ReplaceAll(in.AltGreetings, "\r\n", "\n"), "\n\n") {
			if strings.TrimSpace(g) != "" {
				c.Greetings = append(c.Greetings, strings.TrimSpace(g))
			}
		}
	}
	if in.CarouselEnabled {
		c.CarouselMode = in.CarouselMode
		for _, p := range strings.Split(in.CarouselPrompts, "\n") {
			if strings.TrimSpace(p) != "" {
				c.Carousel = append(c.Carousel, strings.TrimSpace(p))
			}
		}
	}
	notes := []string{"Unsupported samplers, token-based reply cap and per-card compression model are not applied. Unmapped card fields remain inert metadata."}
	for _, a := range []struct {
		value  string
		target *string
	}{{in.Avatar, &c.Portrait}, {in.Background, &c.Background}} {
		if a.value != "" {
			if cast.ValidateImage(a.value) == nil {
				*a.target = a.value
			} else {
				notes = append(notes, "An unsupported, remote or oversized image was omitted; no URL is fetched.")
			}
		}
	}
	var extra map[string]json.RawMessage
	_ = json.Unmarshal(raw, &extra)
	for _, k := range []string{"id", "name", "description", "personality", "avatar", "background", "writingStyle", "startingLore", "ragStorybook", "greeting", "altGreetings", "carouselPrompts", "carouselMode", "textColor", "dialogColor", "contextLimit", "temperature", "topP", "injectionFrequency"} {
		delete(extra, k)
	}
	if len(extra) > 0 {
		b, _ := json.Marshal(extra)
		c.Extra = string(b)
	}
	if err := c.Validate(); err != nil {
		return c, "", nil, fmt.Errorf("invalid GobboNet card: %w", err)
	}
	return c, in.ID, notes, nil
}
