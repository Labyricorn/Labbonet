// Package profile defines settings profiles: a named bundle of the choices a person
// switches between (theme, memory behaviour, reply limits). It is pure validation; the
// store persists bundles and the API applies them.
// Original implementation: Copyright (C) 2026 Labbonet contributors. AGPL-3.0-or-later.
// Behavior reference (named profiles and presets selected from the dashboard): GobboNet
// js/06-state-sync.js and js/21-data.js @ 314bdc9cae204ae38fdc1002ac6a9fb1437c109f,
// Copyright (C) 2026 Elodine / GoblinCorps. No upstream source copied; Labbonet profiles are
// settings bundles in one database, not separate data partitions (docs/REUSE-LOG.md).
package profile

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/Labyricorn/Labbonet/internal/memory"
)

// Limits.
const (
	MaxProfiles  = 20
	MaxNameRunes = 40

	minReplyChars     = 100
	maxReplyChars     = 200000
	minReasoningChars = 500
	maxReasoningChars = 1000000
)

var errInvalid = errors.New("invalid profile")

// IsInvalid reports whether err came from Validate.
func IsInvalid(err error) bool { return errors.Is(err, errInvalid) }

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errInvalid, fmt.Sprintf(format, args...))
}

// Prefs are the per-browser chat limits a profile carries; the browser stores them locally.
type Prefs struct {
	ReplyCap       int  `json:"reply_cap"`
	ReasoningLimit int  `json:"reasoning_limit"`
	Follow         bool `json:"follow"`
}

// Data is what a profile holds. Appearance is the active theme preset id ("" is the default
// theme). Memory is the whole memory-settings object, or nil to leave memory unchanged.
type Data struct {
	Appearance string           `json:"appearance"`
	Memory     *memory.Settings `json:"memory,omitempty"`
	Prefs      Prefs            `json:"prefs"`
}

// Profile is a stored bundle.
type Profile struct {
	ID        int64  `json:"id"`
	Revision  int64  `json:"revision"`
	Name      string `json:"name"`
	Data      Data   `json:"data"`
	UpdatedAt string `json:"updated_at"`
}

var appearanceID = regexp.MustCompile(`^[A-Za-z0-9_-]{0,24}$`)

// Validate checks the name and every value, normalising the name in place.
func (p *Profile) Validate() error {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" || utf8.RuneCountInString(p.Name) > MaxNameRunes || strings.ContainsAny(p.Name, "\x00\r\n\t") {
		return invalid("a name is one line of 1 to %d characters", MaxNameRunes)
	}
	return p.Data.Validate()
}

// Validate checks the bundle.
func (d Data) Validate() error {
	if !appearanceID.MatchString(d.Appearance) {
		return invalid("the theme choice is malformed")
	}
	if c := d.Prefs.ReplyCap; c != 0 && (c < minReplyChars || c > maxReplyChars) {
		return invalid("reply cap must be 0 or %d to %d characters", minReplyChars, maxReplyChars)
	}
	if c := d.Prefs.ReasoningLimit; c != 0 && (c < minReasoningChars || c > maxReasoningChars) {
		return invalid("reasoning limit must be 0 or %d to %d characters", minReasoningChars, maxReasoningChars)
	}
	if d.Memory != nil {
		if err := d.Memory.Validate(); err != nil {
			return invalid("%v", err)
		}
	}
	return nil
}
