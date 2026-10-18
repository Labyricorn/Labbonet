// Package cast defines inert character/persona data shared by storage and prompt assembly.
// Original implementation: Copyright (C) 2026 Labbonet contributors. AGPL-3.0-or-later.
// Card/persona field concepts and frequency-zero semantics reference GobboNet
// chat.html/17-personas.js @ 314bdc9cae204ae38fdc1002ac6a9fb1437c109f,
// Copyright (C) 2026 Elodine / GoblinCorps. No upstream source copied.
// See docs/REUSE-LOG.md for the design/implementation boundary.
package cast

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"regexp"
	"strings"
)

type Card struct {
	ID               int64             `json:"id"`
	Kind             string            `json:"kind"`
	Revision         int64             `json:"revision"`
	Position         int               `json:"position"`
	Name             string            `json:"name"`
	Description      string            `json:"description"`
	Personality      string            `json:"personality"`
	Style            string            `json:"style"`
	SystemPrompt     string            `json:"system_prompt"`
	Greetings        []string          `json:"greetings"`
	Carousel         []string          `json:"carousel"`
	CarouselMode     string            `json:"carousel_mode"`
	Macros           map[string]string `json:"macros"`
	PersonaFrequency int               `json:"persona_frequency"`
	Lore             string            `json:"lore"`
	ContextLimit     int               `json:"context_limit"`
	ReplyLimit       int               `json:"reply_limit"`
	Temperature      *float64          `json:"temperature"`
	TopP             *float64          `json:"top_p"`
	Portrait         string            `json:"portrait"`
	Background       string            `json:"background"`
	TextColor        string            `json:"text_color"`
	DialogColor      string            `json:"dialog_color"`
}

var color = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
var macroName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,63}$`)

// Validate refuses executable/remote assets and invalid parameters at the trust boundary.
func (c Card) Validate() error {
	if c.Kind != "character" && c.Kind != "persona" {
		return errors.New("kind must be character or persona")
	}
	if strings.TrimSpace(c.Name) == "" || len(c.Name) > 200 {
		return errors.New("name is required (maximum 200 bytes)")
	}
	if c.Position < -1000000 || c.Position > 1000000 {
		return errors.New("position out of range")
	}
	for _, v := range []string{c.Description, c.Personality, c.Style, c.SystemPrompt, c.Lore} {
		if len(v) > 65536 {
			return errors.New("text field exceeds 64 KiB")
		}
	}
	if len(c.Greetings) > 64 || len(c.Carousel) > 64 || len(c.Macros) > 64 {
		return errors.New("maximum 64 greetings, carousel entries or macros")
	}
	for _, list := range [][]string{c.Greetings, c.Carousel} {
		for _, v := range list {
			if len(v) > 16384 {
				return errors.New("entry exceeds 16 KiB")
			}
		}
	}
	for k, v := range c.Macros {
		if !macroName.MatchString(k) || len(v) > 16384 {
			return errors.New("invalid macro name or value too large")
		}
		switch strings.ToLower(k) {
		case "char", "user", "date", "time", "carousel":
			return errors.New("reserved macro name")
		}
	}
	if c.CarouselMode != "" && c.CarouselMode != "sequential" && c.CarouselMode != "random" {
		return errors.New("invalid carousel mode")
	}
	if c.PersonaFrequency < 0 || c.PersonaFrequency > 1000 {
		return errors.New("persona frequency must be 0 (disabled) through 1000 turns")
	}
	if c.ContextLimit != 0 && (c.ContextLimit < 256 || c.ContextLimit > 2000000) {
		return errors.New("context limit must be 256 through 2000000, or zero for provider default")
	}
	if c.ReplyLimit < 0 || c.ReplyLimit > 32768 {
		return errors.New("reply limit must be 0 through 32768")
	}
	if c.Temperature != nil && (math.IsNaN(*c.Temperature) || math.IsInf(*c.Temperature, 0) || *c.Temperature < 0 || *c.Temperature > 2) {
		return errors.New("temperature must be 0 through 2")
	}
	if c.TopP != nil && (math.IsNaN(*c.TopP) || math.IsInf(*c.TopP, 0) || *c.TopP <= 0 || *c.TopP > 1) {
		return errors.New("top_p must be greater than 0 and at most 1")
	}
	for _, v := range []string{c.TextColor, c.DialogColor} {
		if v != "" && !color.MatchString(v) {
			return errors.New("colors must be #RRGGBB")
		}
	}
	for _, v := range []string{c.Portrait, c.Background} {
		if err := ValidateImage(v); err != nil {
			return err
		}
	}
	return nil
}

// Images are embedded, bounded PNG/JPEG only. SVG, paths and URLs cannot fetch or execute.
func ValidateImage(s string) error {
	if s == "" {
		return nil
	}
	if len(s) > 1400000 {
		return errors.New("image exceeds 1 MiB")
	}
	prefix, payload, ok := strings.Cut(s, ",")
	if !ok || (prefix != "data:image/png;base64" && prefix != "data:image/jpeg;base64") {
		return errors.New("image must be an embedded PNG or JPEG")
	}
	b, err := base64.StdEncoding.DecodeString(payload)
	if err != nil || len(b) > 1<<20 {
		return errors.New("invalid image encoding or image exceeds 1 MiB")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 4096 || cfg.Height > 4096 || int64(cfg.Width)*int64(cfg.Height) > 16000000 {
		return errors.New("invalid image or dimensions exceed 4096 / 16 megapixels")
	}
	if prefix != "data:image/"+format+";base64" {
		return fmt.Errorf("image media type does not match %s", format)
	}
	if _, _, err := image.Decode(bytes.NewReader(b)); err != nil {
		return errors.New("invalid or truncated image")
	}
	return nil
}

// Identity is copied onto messages, never resolved against a mutable card at render time.
type Identity struct {
	ID          int64  `json:"id,omitempty"`
	Revision    int64  `json:"revision,omitempty"`
	Name        string `json:"name,omitempty"`
	Portrait    string `json:"portrait,omitempty"`
	TextColor   string `json:"text_color,omitempty"`
	DialogColor string `json:"dialog_color,omitempty"`
}

func Speaker(c *Card) Identity {
	if c == nil {
		return Identity{}
	}
	return Identity{c.ID, c.Revision, c.Name, c.Portrait, c.TextColor, c.DialogColor}
}
