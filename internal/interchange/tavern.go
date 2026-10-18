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
	NativeCardFormat = "labbonet-card"
	labbonetExtKey   = "labbonet"
)

// parsedCard is one imported card plus the compatibility notes shown at preview.
type parsedCard struct {
	Card   cast.Card
	Format string
	Notes  []string
	Icon   string // embedded asset URI named by the card, if any
}

// tavernExtra lists top-level V2/V3 data keys kept verbatim as inert metadata.
var tavernExtra = []string{"creator_notes", "tags", "creator", "character_version", "post_history_instructions", "nickname", "source", "creation_date", "modification_date", "group_only_greetings", "creator_notes_multilingual"}

// labbonetExt carries fields that have no Tavern equivalent, so a Labbonet export
// round-trips through other tools. Only these keys are ever read back.
type labbonetExt struct {
	Kind             string            `json:"kind"`
	Style            *string           `json:"style"`
	Carousel         []string          `json:"carousel"`
	CarouselMode     string            `json:"carousel_mode"`
	Macros           map[string]string `json:"macros"`
	PersonaFrequency *int              `json:"persona_frequency"`
	ContextLimit     int               `json:"context_limit"`
	ReplyLimit       int               `json:"reply_limit"`
	Temperature      *float64          `json:"temperature"`
	TopP             *float64          `json:"top_p"`
	TextColor        string            `json:"text_color"`
	DialogColor      string            `json:"dialog_color"`
	Background       string            `json:"background"`
	Storybook        string            `json:"storybook"`
}

func rawString(m map[string]json.RawMessage, key string) string {
	var s string
	if raw, ok := m[key]; ok && json.Unmarshal(raw, &s) == nil {
		return s
	}
	return ""
}

func rawStrings(m map[string]json.RawMessage, key string) []string {
	var list []string
	if raw, ok := m[key]; ok && json.Unmarshal(raw, &list) == nil {
		return list
	}
	return nil
}

func decodeObject(raw []byte) (map[string]json.RawMessage, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil || m == nil {
		return nil, errors.New("card is not a valid JSON object")
	}
	return m, nil
}

// parseCardJSON understands the native format and Tavern V1, V2 and V3 JSON.
func parseCardJSON(raw []byte) (*parsedCard, error) {
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	if len(raw) > maxCardJSON {
		return nil, errors.New("card JSON is too large")
	}
	top, err := decodeObject(raw)
	if err != nil {
		return nil, err
	}
	if rawString(top, "format") == NativeCardFormat {
		return parseNative(raw)
	}
	data := top
	format := "Tavern V1"
	switch spec := rawString(top, "spec"); spec {
	case "chara_card_v2", "chara_card_v3":
		inner, ok := top["data"]
		if !ok {
			return nil, errors.New("card has no data object")
		}
		if data, err = decodeObject(inner); err != nil {
			return nil, err
		}
		format = map[string]string{"chara_card_v2": "Tavern V2", "chara_card_v3": "Tavern V3"}[spec]
	case "":
		if rawString(top, "name") == "" {
			return nil, errors.New("unrecognized card: no spec and no name")
		}
	default:
		return nil, fmt.Errorf("unsupported card spec %q", truncate(spec, 40))
	}
	return mapTavern(data, format)
}

func parseNative(raw []byte) (*parsedCard, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var env struct {
		Format  string    `json:"format"`
		Version int       `json:"version"`
		Card    cast.Card `json:"card"`
	}
	if err := dec.Decode(&env); err != nil {
		return nil, fmt.Errorf("invalid native card: %w", err)
	}
	if env.Version != 1 {
		return nil, errors.New("unsupported native card version")
	}
	return &parsedCard{Card: env.Card, Format: "Labbonet card"}, nil
}

func mustMarshal(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func mapTavern(d map[string]json.RawMessage, format string) (*parsedCard, error) {
	name := strings.TrimSpace(rawString(d, "name"))
	if name == "" {
		return nil, errors.New("card has no name")
	}
	p := &parsedCard{Format: format}
	c := &p.Card
	c.Kind, c.Name = "character", name
	c.Description = rawString(d, "description")
	if scenario := rawString(d, "scenario"); strings.TrimSpace(scenario) != "" {
		if c.Description != "" {
			c.Description += "\n\n"
		}
		c.Description += "Scenario: " + scenario
		p.Notes = append(p.Notes, "scenario appended to the description")
	}
	c.Personality = rawString(d, "personality")
	c.SystemPrompt = rawString(d, "system_prompt")
	c.Style = rawString(d, "mes_example")
	if c.Style != "" {
		p.Notes = append(p.Notes, "example dialogue stored as writing style")
	}
	if first := rawString(d, "first_mes"); strings.TrimSpace(first) != "" {
		c.Greetings = append(c.Greetings, first)
	}
	for _, g := range rawStrings(d, "alternate_greetings") {
		if strings.TrimSpace(g) != "" {
			c.Greetings = append(c.Greetings, g)
		}
	}
	if raw, ok := d["character_book"]; ok && string(raw) != "null" {
		if lore := loreFromBook(raw); lore != "" {
			c.Lore = lore
			p.Notes = append(p.Notes, "character book entries merged into authored lore; keyword activation is not preserved")
		}
	}
	extra := map[string]json.RawMessage{}
	for _, k := range tavernExtra {
		if raw, ok := d[k]; ok && string(raw) != "null" && string(raw) != `""` && string(raw) != "[]" {
			extra[k] = raw
		}
	}
	var ext map[string]json.RawMessage
	if raw, ok := d["extensions"]; ok {
		ext, _ = decodeObject(raw)
	}
	if rawLab, ok := ext[labbonetExtKey]; ok {
		applyLabbonetExt(c, rawLab, p)
		delete(ext, labbonetExtKey)
	}
	if len(ext) > 0 {
		extra["extensions"] = mustMarshal(ext)
		for _, risky := range []string{"regex_scripts", "scripts", "triggers", "triggerscript", "risuai", "depth_prompt"} {
			if _, ok := ext[risky]; ok {
				p.Notes = append(p.Notes, fmt.Sprintf("extension %q kept as inert data and never executed", risky))
			}
		}
	}
	if len(extra) > 0 {
		if b := mustMarshal(extra); len(b) <= 65536 {
			c.Extra = string(b)
			p.Notes = append(p.Notes, "unmapped metadata kept inertly for export")
		} else {
			p.Notes = append(p.Notes, "unmapped metadata exceeded 64 KiB and was dropped")
		}
	}
	if raw, ok := d["assets"]; ok {
		p.Icon = iconURI(raw)
	}
	return p, nil
}

func loreFromBook(raw json.RawMessage) string {
	var book struct {
		Entries []struct {
			Content string `json:"content"`
			Enabled *bool  `json:"enabled"`
		} `json:"entries"`
	}
	if json.Unmarshal(raw, &book) != nil {
		return ""
	}
	var parts []string
	for _, e := range book.Entries {
		if (e.Enabled == nil || *e.Enabled) && strings.TrimSpace(e.Content) != "" {
			parts = append(parts, e.Content)
		}
	}
	return strings.Join(parts, "\n\n")
}

func iconURI(raw json.RawMessage) string {
	var assets []struct {
		Type string `json:"type"`
		URI  string `json:"uri"`
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &assets) != nil {
		return ""
	}
	best := ""
	for _, a := range assets {
		if a.Type == "icon" && best == "" {
			best = a.URI
		}
		if a.Type == "icon" && a.Name == "main" {
			return a.URI
		}
	}
	return best
}

func applyLabbonetExt(c *cast.Card, raw json.RawMessage, p *parsedCard) {
	var e labbonetExt
	if json.Unmarshal(raw, &e) != nil {
		p.Notes = append(p.Notes, "Labbonet extension block was malformed and ignored")
		return
	}
	if e.Kind == "persona" {
		c.Kind = "persona"
	}
	if e.Style != nil {
		c.Style = *e.Style
	}
	c.Carousel, c.CarouselMode, c.Macros = e.Carousel, e.CarouselMode, e.Macros
	if e.PersonaFrequency != nil {
		c.PersonaFrequency = *e.PersonaFrequency
	}
	c.ContextLimit, c.ReplyLimit = e.ContextLimit, e.ReplyLimit
	c.Temperature, c.TopP = e.Temperature, e.TopP
	c.TextColor, c.DialogColor, c.Background = e.TextColor, e.DialogColor, e.Background
	c.Storybook = e.Storybook
}

// ---- export ----

func cardData(c cast.Card, v3 bool) map[string]any {
	ext := map[string]any{}
	extra := map[string]json.RawMessage{}
	if c.Extra != "" {
		_ = json.Unmarshal([]byte(c.Extra), &extra)
	}
	if raw, ok := extra["extensions"]; ok {
		_ = json.Unmarshal(raw, &ext)
	}
	ext[labbonetExtKey] = labbonetExt{
		Kind: c.Kind, Style: &c.Style, Carousel: c.Carousel, CarouselMode: c.CarouselMode, Macros: c.Macros,
		PersonaFrequency: &c.PersonaFrequency, ContextLimit: c.ContextLimit, ReplyLimit: c.ReplyLimit,
		Temperature: c.Temperature, TopP: c.TopP, TextColor: c.TextColor, DialogColor: c.DialogColor, Background: c.Background, Storybook: c.Storybook,
	}
	first, alt := "", []string{}
	if len(c.Greetings) > 0 {
		first, alt = c.Greetings[0], append(alt, c.Greetings[1:]...)
	}
	d := map[string]any{
		"name": c.Name, "description": c.Description, "personality": c.Personality, "scenario": "",
		"first_mes": first, "mes_example": c.Style, "creator_notes": "", "system_prompt": c.SystemPrompt,
		"post_history_instructions": "", "alternate_greetings": alt, "tags": []string{}, "creator": "",
		"character_version": "", "extensions": ext,
	}
	for _, k := range tavernExtra {
		if raw, ok := extra[k]; ok {
			var v any
			if json.Unmarshal(raw, &v) == nil {
				d[k] = v
			}
		}
	}
	if c.Lore != "" {
		d["character_book"] = map[string]any{"entries": []map[string]any{{
			"keys": []string{}, "content": c.Lore, "enabled": true, "insertion_order": 0, "constant": true, "extensions": map[string]any{},
		}}}
	}
	if v3 {
		d["group_only_greetings"] = []string{}
		if raw, ok := extra["group_only_greetings"]; ok {
			var v any
			if json.Unmarshal(raw, &v) == nil {
				d["group_only_greetings"] = v
			}
		}
		d["assets"] = []map[string]any{{"type": "icon", "uri": "ccdefault:", "name": "main", "ext": "png"}}
	}
	return d
}

func exportSpec(c cast.Card, v3 bool) []byte {
	spec, version := "chara_card_v2", "2.0"
	if v3 {
		spec, version = "chara_card_v3", "3.0"
	}
	b, _ := json.MarshalIndent(map[string]any{"spec": spec, "spec_version": version, "data": cardData(c, v3)}, "", "  ")
	return b
}

// ExportNative returns the lossless Labbonet card envelope. Local IDs, revisions and
// positions are never exported.
func ExportNative(c cast.Card) []byte {
	c.ID, c.Revision, c.Position = 0, 0, 0
	b, _ := json.MarshalIndent(map[string]any{"format": NativeCardFormat, "version": 1, "card": c}, "", "  ")
	return b
}

// ExportV3JSON and ExportV2JSON write the Tavern formats.
func ExportV3JSON(c cast.Card) []byte { return exportSpec(c, true) }
func ExportV2JSON(c cast.Card) []byte { return exportSpec(c, false) }
