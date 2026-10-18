package mods

import (
	"fmt"
	"strings"
	"testing"
)

func TestValidateOne(t *testing.T) {
	ok := Macro{Trigger: "fast_forward", Text: "Skip ahead one scene."}
	if err := ValidateOne(ok); err != nil {
		t.Fatal(err)
	}
	bad := map[string]Macro{
		"empty trigger":  {Trigger: "", Text: "x"},
		"digit first":    {Trigger: "1abc", Text: "x"},
		"space":          {Trigger: "a b", Text: "x"},
		"braces":         {Trigger: "{{a}}", Text: "x"},
		"too long":       {Trigger: "a" + strings.Repeat("b", MaxTriggerLen), Text: "x"},
		"reserved":       {Trigger: "char", Text: "x"},
		"reserved case":  {Trigger: "USER", Text: "x"},
		"reserved later": {Trigger: "auto_continue", Text: "x"},
		"blank text":     {Trigger: "a", Text: " \n "},
		"huge text":      {Trigger: "a", Text: strings.Repeat("x", MaxMacroBytes+1)},
		"nul":            {Trigger: "a", Text: "x\x00y"},
		"bad utf8":       {Trigger: "a", Text: "x\xffy"},
	}
	for name, m := range bad {
		if err := ValidateOne(m); !IsInvalid(err) {
			t.Errorf("%s accepted: %v", name, err)
		}
	}
}

func chain(n int) []Macro {
	out := []Macro{}
	for i := 0; i < n; i++ {
		text := "end"
		if i+1 < n {
			text = fmt.Sprintf("{{m%d}}", i+1)
		}
		out = append(out, Macro{Trigger: fmt.Sprintf("m%d", i), Text: text})
	}
	return out
}

func TestValidateSet(t *testing.T) {
	good := []Macro{{Trigger: "a", Text: "one {{b}}"}, {Trigger: "b", Text: "two"}}
	if err := ValidateSet(good); err != nil {
		t.Fatal(err)
	}
	cases := map[string][]Macro{
		"duplicate ignoring case": {{Trigger: "Foo", Text: "x"}, {Trigger: "foo", Text: "y"}},
		"direct cycle":            {{Trigger: "a", Text: "{{a}}"}},
		"two cycle":               {{Trigger: "a", Text: "{{b}}"}, {Trigger: "b", Text: "x {{a}}"}},
		"cycle ignoring case":     {{Trigger: "a", Text: "{{B}}"}, {Trigger: "b", Text: "{{A}}"}},
		"too deep":                chain(30),
	}
	for name, set := range cases {
		if err := ValidateSet(set); !IsInvalid(err) {
			t.Errorf("%s accepted: %v", name, err)
		}
	}
	if err := ValidateSet(chain(10)); err != nil {
		t.Errorf("a ten-step chain is fine: %v", err)
	}
	many := make([]Macro, MaxMacros+1)
	for i := range many {
		many[i] = Macro{Trigger: fmt.Sprintf("m%d", i), Text: "x"}
	}
	if err := ValidateSet(many); !IsInvalid(err) {
		t.Errorf("too many accepted: %v", err)
	}
	// Exponential growth is stopped by the prompt's expansion size limit, not just depth.
	bomb := []Macro{{Trigger: "a", Text: strings.Repeat("{{b}}", 40)}}
	for i, name := range []string{"b", "c", "d", "e"} {
		next := []string{"c", "d", "e", "f"}[i]
		bomb = append(bomb, Macro{Trigger: name, Text: strings.Repeat("{{"+next+"}}", 40)})
	}
	bomb = append(bomb, Macro{Trigger: "f", Text: strings.Repeat("y", 4000)})
	if err := ValidateSet(bomb); !IsInvalid(err) {
		t.Errorf("expansion bomb accepted: %v", err)
	}
}

func TestValidateVarsAllowlist(t *testing.T) {
	if err := ValidateVars(map[string]string{"--cyan-bright": "#00F3ff", "--font-scale": "1.2", "--avatar-scale": "0.5"}); err != nil {
		t.Fatal(err)
	}
	for name, v := range map[string]map[string]string{
		"unknown property": {"--evil": "#000000"},
		"real property":    {"background-image": "#000000"},
		"url value":        {"--black": "url(http://example.org/x.png)"},
		"named colour":     {"--black": "red"},
		"short hex":        {"--black": "#fff"},
		"alpha hex":        {"--black": "#00000000"},
		"injection":        {"--black": "#000000;color:red"},
		"scale low":        {"--font-scale": "0.1"},
		"scale high":       {"--avatar-scale": "9"},
		"scale nan":        {"--font-scale": "NaN"},
		"scale text":       {"--font-scale": "big"},
		"scale units":      {"--font-scale": "1.2px"},
	} {
		if err := ValidateVars(v); !IsInvalid(err) {
			t.Errorf("%s accepted: %v", name, err)
		}
	}
}

func TestBuiltinPresetsAreValid(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range Builtin() {
		if seen[p.ID] || !p.Builtin {
			t.Errorf("bad built-in %q", p.ID)
		}
		seen[p.ID] = true
		if err := ValidateVars(p.Vars); err != nil {
			t.Errorf("built-in %s: %v", p.ID, err)
		}
	}
	if !seen["default"] || len(Builtin()[0].Vars) != 0 {
		t.Error("default preset must exist and override nothing")
	}
}

func TestAppearanceValidate(t *testing.T) {
	custom := Preset{ID: "c-1", Name: "Mine", Vars: map[string]string{"--magenta": "#ff00aa"}}
	good := Appearance{Active: "c-1", Presets: []Preset{custom}}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := good.Effective()["--magenta"]; got != "#ff00aa" {
		t.Errorf("effective = %v", good.Effective())
	}
	if got := (Appearance{Active: "amber"}).Effective()["--cyan-bright"]; got == "" {
		t.Error("built-in effective missing")
	}
	bad := map[string]Appearance{
		"unknown active":   {Active: "nope"},
		"builtin id reuse": {Presets: []Preset{{ID: "amber", Name: "x", Vars: map[string]string{}}}},
		"duplicate id":     {Presets: []Preset{custom, custom}},
		"empty name":       {Presets: []Preset{{ID: "c-2", Name: " ", Vars: map[string]string{}}}},
		"newline name":     {Presets: []Preset{{ID: "c-2", Name: "a\nb", Vars: map[string]string{}}}},
		"bad id":           {Presets: []Preset{{ID: "C 2", Name: "x", Vars: map[string]string{}}}},
		"bad vars":         {Presets: []Preset{{ID: "c-2", Name: "x", Vars: map[string]string{"--black": "javascript:1"}}}},
	}
	for name, a := range bad {
		if err := a.Validate(); !IsInvalid(err) {
			t.Errorf("%s accepted: %v", name, err)
		}
	}
	tooMany := Appearance{}
	for i := 0; i <= MaxPresets; i++ {
		tooMany.Presets = append(tooMany.Presets, Preset{ID: fmt.Sprintf("c-%d", i), Name: "x", Vars: map[string]string{}})
	}
	if err := tooMany.Validate(); !IsInvalid(err) {
		t.Errorf("too many presets accepted: %v", err)
	}
}
