// Package mods holds Labbonet's MODS data: global text macros and declarative appearance
// presets. Both are plain validated data. A macro is text that expands into the prompt; an
// appearance preset is a set of allowlisted colour and number values the browser applies
// through the CSSOM. Neither can carry script, a URL or a stylesheet, so MODS cannot fetch
// or execute anything (the intentional parity exception for GobboNet's extension loader).
// Original implementation: Copyright (C) 2026 Labbonet contributors. AGPL-3.0-or-later.
// Behavior/design reference: GobboNet js/20-macros.js and js/17-personas.js (resolveMacros)
// @ 314bdc9cae204ae38fdc1002ac6a9fb1437c109f, Copyright (C) 2026 Elodine / GoblinCorps.
// No upstream source copied. Exact attribution boundaries: docs/REUSE-LOG.md.
package mods

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Labyricorn/Labbonet/internal/prompt"
)

var errInvalid = errors.New("invalid MODS data")

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errInvalid, fmt.Sprintf(format, args...))
}

// IsInvalid reports whether err came from validation in this package.
func IsInvalid(err error) bool { return errors.Is(err, errInvalid) }

// Macro limits.
const (
	MaxMacros      = 100
	MaxTriggerLen  = 40
	MaxMacroBytes  = 8 << 10
	MaxMacroTotal  = 128 << 10
	PreviewMaxSize = 16 << 10
)

// Reserved names are built into the prompt (or planned for the scheduler) and cannot be
// redefined. Comparison is case-insensitive, as macro lookup is.
var Reserved = []string{"char", "user", "date", "time", "carousel", "current_dat", "auto_continue"}

var triggerPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)

// Macro is one user-defined {{trigger}} → text expansion.
type Macro struct {
	ID        int64  `json:"id"`
	Revision  int64  `json:"revision"`
	Trigger   string `json:"trigger"`
	Text      string `json:"text"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// ValidateOne checks a single macro's shape (not its interaction with the others).
func ValidateOne(m Macro) error {
	if m.Trigger == "" || len(m.Trigger) > MaxTriggerLen || !triggerPattern.MatchString(m.Trigger) {
		return invalid("a trigger starts with a letter and uses only letters, digits and underscores (at most %d characters)", MaxTriggerLen)
	}
	for _, r := range Reserved {
		if strings.EqualFold(m.Trigger, r) {
			return invalid("{{%s}} is a built-in macro and cannot be redefined", m.Trigger)
		}
	}
	if strings.TrimSpace(m.Text) == "" {
		return invalid("the expansion text cannot be empty")
	}
	if len(m.Text) > MaxMacroBytes {
		return invalid("a macro expansion is limited to %d KiB", MaxMacroBytes>>10)
	}
	if !utf8.ValidString(m.Text) || strings.ContainsRune(m.Text, 0) {
		return invalid("the expansion text must be valid text")
	}
	return nil
}

// ValidateSet checks the whole set: count, total size, unique triggers (ignoring case) and
// that every expansion terminates within the prompt's expansion limits (no cycles).
func ValidateSet(macros []Macro) error {
	if len(macros) > MaxMacros {
		return invalid("at most %d macros", MaxMacros)
	}
	seen := map[string]bool{}
	total := 0
	for _, m := range macros {
		if err := ValidateOne(m); err != nil {
			return err
		}
		key := strings.ToLower(m.Trigger)
		if seen[key] {
			return invalid("a macro named {{%s}} already exists", m.Trigger)
		}
		seen[key] = true
		total += len(m.Text)
	}
	if total > MaxMacroTotal {
		return invalid("macros are limited to %d KiB in total", MaxMacroTotal>>10)
	}
	values := Values(macros)
	for _, m := range macros {
		if _, err := prompt.Expand(m.Text, values); err != nil {
			return invalid("{{%s}} cannot be expanded: %v", m.Trigger, err)
		}
	}
	return nil
}

// Values turns macros into the lowercase-keyed map the prompt expands with.
func Values(macros []Macro) map[string]string {
	out := make(map[string]string, len(macros))
	for _, m := range macros {
		out[strings.ToLower(m.Trigger)] = m.Text
	}
	return out
}

// ---- appearance -------------------------------------------------------------------

// Appearance limits.
const (
	MaxPresets    = 12
	MaxPresetName = 40
)

type varSpec struct {
	color    bool
	min, max float64
}

// Vars is the complete allowlist of settings a preset may carry. Each name is a CSS custom
// property the pinned stylesheets already read (or, for --font-scale, app.css reads).
var Vars = map[string]varSpec{
	"--black": {color: true}, "--surface": {color: true}, "--elevated": {color: true}, "--border": {color: true},
	"--cyan-bright": {color: true}, "--cyan-mid": {color: true}, "--cyan-dim": {color: true},
	"--magenta": {color: true}, "--magenta-dim": {color: true}, "--white": {color: true},
	"--avatar-scale": {min: 0.5, max: 2}, "--font-scale": {min: 0.8, max: 1.4},
}

// VarNames lists the allowlist in a stable order.
func VarNames() []string {
	names := make([]string, 0, len(Vars))
	for n := range Vars {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

var (
	colorPattern  = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	presetPattern = regexp.MustCompile(`^[a-z0-9-]+$`)
)

// ValidateVars checks a preset's values against the allowlist. Colours are #rrggbb;
// numbers are finite and in range.
func ValidateVars(vars map[string]string) error {
	if len(vars) > len(Vars) {
		return invalid("too many settings")
	}
	for name, value := range vars {
		spec, ok := Vars[name]
		if !ok {
			return invalid("%q is not an adjustable appearance setting", name)
		}
		if spec.color {
			if !colorPattern.MatchString(value) {
				return invalid("%s must be a colour like #00f3ff", name)
			}
			continue
		}
		n, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < spec.min || n > spec.max || len(value) > 8 {
			return invalid("%s must be a number from %g to %g", name, spec.min, spec.max)
		}
	}
	return nil
}

// Preset is a named set of appearance values. Built-in presets are fixed in code; custom
// ones are stored.
type Preset struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Vars    map[string]string `json:"vars"`
	Builtin bool              `json:"builtin,omitempty"`
}

// Builtin presets, in display order. "default" has no overrides: it is the pinned theme.
func Builtin() []Preset {
	return []Preset{
		{ID: "default", Name: "Neon (default)", Vars: map[string]string{}, Builtin: true},
		{ID: "amber", Name: "Amber terminal", Builtin: true, Vars: map[string]string{
			"--cyan-bright": "#ffb000", "--cyan-mid": "#e69a00", "--cyan-dim": "#7a4f00", "--magenta": "#ff6a00", "--magenta-dim": "#6b2d00",
			"--white": "#fff1d6", "--black": "#050400", "--surface": "#0d0a02", "--elevated": "#1a1404", "--border": "#3a2c08"}},
		{ID: "ice", Name: "Ice", Builtin: true, Vars: map[string]string{
			"--cyan-bright": "#9be7ff", "--cyan-mid": "#6cc3e6", "--cyan-dim": "#2f6a85", "--magenta": "#b39dff", "--magenta-dim": "#41366f",
			"--white": "#f0faff", "--black": "#03060a", "--surface": "#080e16", "--elevated": "#0e1824", "--border": "#1d3347"}},
		{ID: "green", Name: "Green phosphor", Builtin: true, Vars: map[string]string{
			"--cyan-bright": "#33ff66", "--cyan-mid": "#22cc55", "--cyan-dim": "#116630", "--magenta": "#a8ff33", "--magenta-dim": "#3d6611",
			"--white": "#e0ffe8", "--black": "#010501", "--surface": "#040d06", "--elevated": "#07180b", "--border": "#0f3a1a"}},
		{ID: "large", Name: "Large text", Builtin: true, Vars: map[string]string{"--font-scale": "1.2", "--avatar-scale": "1.25"}},
	}
}

// Appearance is the stored selection plus custom presets.
type Appearance struct {
	Revision int64    `json:"revision"`
	Active   string   `json:"active"`
	Presets  []Preset `json:"presets"`
}

// Validate checks custom presets and that Active names a preset that exists. It does not
// assign IDs; Normalize does.
func (a Appearance) Validate() error {
	if len(a.Presets) > MaxPresets {
		return invalid("at most %d custom presets", MaxPresets)
	}
	ids := map[string]bool{}
	for _, b := range Builtin() {
		ids[b.ID] = true
	}
	for _, p := range a.Presets {
		if p.ID == "" || len(p.ID) > 24 || !presetPattern.MatchString(p.ID) {
			return invalid("a preset id is missing or malformed")
		}
		if ids[p.ID] {
			return invalid("duplicate or reserved preset id %q", p.ID)
		}
		ids[p.ID] = true
		name := strings.TrimSpace(p.Name)
		if name == "" || utf8.RuneCountInString(name) > MaxPresetName || strings.ContainsAny(name, "\x00\r\n\t") {
			return invalid("a preset name is one line of 1 to %d characters", MaxPresetName)
		}
		if err := ValidateVars(p.Vars); err != nil {
			return err
		}
	}
	if a.Active != "" && !ids[a.Active] {
		return invalid("the selected preset does not exist")
	}
	return nil
}

// Effective returns the values to apply for the active preset (nil for the default).
func (a Appearance) Effective() map[string]string {
	for _, p := range Builtin() {
		if p.ID == a.Active {
			return p.Vars
		}
	}
	for _, p := range a.Presets {
		if p.ID == a.Active {
			return p.Vars
		}
	}
	return map[string]string{}
}
