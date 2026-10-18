package interchange

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Labyricorn/Labbonet/internal/cast"
)

func sampleCard() cast.Card {
	temp := 0.7
	return cast.Card{
		Kind: "character", Name: "Mira", Description: "A <b>wanderer</b>", Personality: "curious", Style: "terse",
		SystemPrompt: "Stay in character.", Greetings: []string{"Hello", "Hi again"}, Carousel: []string{"a", "b"},
		CarouselMode: "random", Macros: map[string]string{"place": "inn"}, Lore: "The inn burned down.",
		Storybook:    "# innkeeper\ntags: innkeeper [1.0], tavern\nHe pours the ale.\n",
		ContextLimit: 4096, ReplyLimit: 300, Temperature: &temp, TextColor: "#aabbcc", DialogColor: "#112233",
		Extra: `{"creator_notes":"hi","tags":["a"],"extensions":{"regex_scripts":[{"find":"x"}]}}`,
	}
}

func mustParse(t *testing.T, data []byte) *Parsed {
	t.Helper()
	p, err := Parse(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return p
}

func sameCard(t *testing.T, got, want cast.Card) {
	t.Helper()
	got.ID, got.Revision, got.Position = 0, 0, 0
	want.ID, want.Revision, want.Position = 0, 0, 0
	g, _ := json.Marshal(got)
	w, _ := json.Marshal(want)
	if !bytes.Equal(g, w) {
		t.Fatalf("round trip differs:\n got %s\nwant %s", g, w)
	}
}

func TestRoundTripAllFormats(t *testing.T) {
	want := sampleCard()
	png, err := ExportPNG(want)
	if err != nil {
		t.Fatal(err)
	}
	zipped, err := ExportZIP(want)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"native": ExportNative(want), "v3": ExportV3JSON(want), "v2": ExportV2JSON(want), "png": png, "zip": zipped} {
		got := mustParse(t, data).Snapshot.Cards[0].Card
		if name == "png" || name == "zip" {
			if got.Portrait == "" {
				t.Fatalf("%s: placeholder portrait missing", name)
			}
			got.Portrait = ""
		}
		got.Extra = canonExtra(t, got.Extra)
		w := want
		w.Extra = canonExtra(t, w.Extra)
		sameCard(t, got, w)
	}
}

func canonExtra(t *testing.T, s string) string {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func TestPersonaKindSurvivesTavernEnvelope(t *testing.T) {
	p := cast.Card{Kind: "persona", Name: "Me", Description: "tall", PersonaFrequency: 3}
	got := mustParse(t, ExportV3JSON(p)).Snapshot.Cards[0].Card
	if got.Kind != "persona" || got.PersonaFrequency != 3 {
		t.Fatalf("%+v", got)
	}
}

func TestTavernV1AndV2Mapping(t *testing.T) {
	v1 := `{"name":"Old","description":"d","personality":"p","scenario":"s","first_mes":"hi","mes_example":"ex"}`
	c := mustParse(t, []byte(v1))
	card := c.Snapshot.Cards[0].Card
	if card.Name != "Old" || !strings.Contains(card.Description, "Scenario: s") || card.Greetings[0] != "hi" || card.Style != "ex" {
		t.Fatalf("%+v", card)
	}
	v2 := `{"spec":"chara_card_v2","spec_version":"2.0","data":{"name":"New","first_mes":"a","alternate_greetings":["b","","c"],
	  "character_book":{"entries":[{"content":"lore one","enabled":true},{"content":"off","enabled":false}]},
	  "creator_notes":"note","extensions":{"regex_scripts":[{"x":1}],"world":"w"}}}`
	p := mustParse(t, []byte(v2))
	card = p.Snapshot.Cards[0].Card
	if len(card.Greetings) != 3 || card.Lore != "lore one" {
		t.Fatalf("%+v", card)
	}
	if !strings.Contains(card.Extra, "regex_scripts") || !strings.Contains(card.Extra, "creator_notes") {
		t.Fatalf("extra not preserved: %s", card.Extra)
	}
	joined := strings.Join(p.Items[0].Notes, "|")
	if !strings.Contains(joined, "never executed") || !strings.Contains(joined, "lore") {
		t.Fatalf("report incomplete: %s", joined)
	}
}

func TestRejectMalformedCardJSON(t *testing.T) {
	for _, in := range []string{
		``, `[]`, `null`, `{`, `{"spec":"chara_card_v9","data":{}}`, `{"spec":"chara_card_v2"}`,
		`{"spec":"chara_card_v2","data":{"name":" "}}`, `{"description":"no name"}`,
		`{"format":"labbonet-card","version":2,"card":{}}`,
		`{"format":"labbonet-card","version":1,"card":{"kind":"character","name":"x","bogus":1}}`,
		`{"name":"x","description":"` + strings.Repeat("a", 70000) + `"}`,
		`{"format":"labbonet-card","version":1,"card":{"kind":"character","name":"x","portrait":"https://evil.example/x.png"}}`,
		`{"format":"labbonet-card","version":1,"card":{"kind":"character","name":"x","extra":"[1]"}}`,
	} {
		if _, err := Parse([]byte(in)); err == nil {
			t.Errorf("accepted %.60q", in)
		}
	}
}

func TestPNGMalformed(t *testing.T) {
	good, _ := ExportPNG(sampleCard())
	if _, err := Parse(good); err != nil {
		t.Fatal(err)
	}
	cases := map[string][]byte{
		"truncated":  good[:len(good)/2],
		"signature":  append([]byte("\x89PNG\r\n\x1a\n"), 0, 0, 0),
		"no card":    mustPNG(t, nil),
		"bad base64": mustPNG(t, [][2]string{{"chara", "!!!notbase64"}}),
		"bad json":   mustPNG(t, [][2]string{{"ccv3", base64.StdEncoding.EncodeToString([]byte("{nope"))}}),
	}
	// Corrupt a text-chunk CRC.
	bad := append([]byte(nil), good...)
	i := bytes.Index(bad, []byte("tEXt"))
	end := i + 4 + int(bad[i-1]) + int(bad[i-2])<<8 + int(bad[i-3])<<16
	bad[end+3] ^= 0xff
	cases["crc"] = bad
	// Chunk length larger than the file.
	huge := append([]byte(nil), good...)
	copy(huge[8:12], []byte{0x7f, 0xff, 0xff, 0xff})
	cases["huge chunk"] = huge
	for name, data := range cases {
		if _, err := Parse(data); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func mustPNG(t *testing.T, texts [][2]string) []byte {
	t.Helper()
	clean, err := portraitPNG("")
	if err != nil {
		t.Fatal(err)
	}
	if texts == nil {
		return clean
	}
	out, err := pngWithText(clean, texts)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestPNGPortraitIsStrippedOfEmbeddedCard(t *testing.T) {
	p := mustParse(t, mustPNG(t, [][2]string{{"ccv3", base64.StdEncoding.EncodeToString(ExportV3JSON(sampleCard()))}}))
	_, payload, _ := strings.Cut(p.Snapshot.Cards[0].Card.Portrait, ",")
	raw, _ := base64.StdEncoding.DecodeString(payload)
	if bytes.Contains(raw, []byte("tEXt")) {
		t.Fatal("embedded card metadata leaked into stored portrait")
	}
}

func buildZip(t *testing.T, entries map[string][]byte, order ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	names := order
	if len(names) == 0 {
		for n := range entries {
			names = append(names, n)
		}
	}
	for _, n := range names {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(entries[n])
	}
	zw.Close()
	return buf.Bytes()
}

func TestArchiveRejections(t *testing.T) {
	card := ExportV3JSON(sampleCard())
	if _, err := Parse(buildZip(t, map[string][]byte{"card.json": card})); err != nil {
		t.Fatal(err)
	}
	tooMany := map[string][]byte{"card.json": card}
	for i := 0; i < maxArchiveEntries; i++ {
		tooMany["f"+string(rune('a'+i%26))+strings.Repeat("x", i/26)+".txt"] = []byte("x")
	}
	bomb := make([]byte, 20<<20) // compresses to ~20 KiB
	cases := map[string][]byte{
		"traversal":  buildZip(t, map[string][]byte{"card.json": card, "../evil.txt": []byte("x")}),
		"absolute":   buildZip(t, map[string][]byte{"card.json": card, "/etc/passwd": []byte("x")}),
		"backslash":  buildZip(t, map[string][]byte{"card.json": card, `..\evil.txt`: []byte("x")}),
		"drive":      buildZip(t, map[string][]byte{"card.json": card, "C:/x.txt": []byte("x")}),
		"executable": buildZip(t, map[string][]byte{"card.json": card, "assets/run.exe": []byte("MZ")}),
		"script":     buildZip(t, map[string][]byte{"card.json": card, "module.js": []byte("alert(1)")}),
		"bomb":       buildZip(t, map[string][]byte{"card.json": card, "assets/big.bin": bomb}),
		"no card":    buildZip(t, map[string][]byte{"readme.txt": []byte("x")}),
		"too many":   buildZip(t, tooMany),
		"duplicate":  buildZip(t, map[string][]byte{"card.json": card}, "card.json", "CARD.JSON"),
		"truncated":  buildZip(t, map[string][]byte{"card.json": card})[:40],
		"bad card":   buildZip(t, map[string][]byte{"card.json": []byte("{")}),
	}
	for name, data := range cases {
		if _, err := Parse(data); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestArchiveIgnoresUnknownAssetsAndRemoteIcons(t *testing.T) {
	d := `{"spec":"chara_card_v3","spec_version":"3.0","data":{"name":"Z","assets":[{"type":"icon","uri":"https://evil.example/a.png","name":"main"}]}}`
	p := mustParse(t, buildZip(t, map[string][]byte{"card.json": []byte(d), "assets/x.dat": []byte("1")}))
	joined := strings.Join(p.Items[0].Notes, "|")
	if p.Snapshot.Cards[0].Card.Portrait != "" || !strings.Contains(joined, "not fetched") || !strings.Contains(joined, "ignored") {
		t.Fatalf("%q %q", joined, p.Snapshot.Cards[0].Card.Portrait)
	}
}

func snap(t *testing.T) Snapshot {
	return Snapshot{Format: SnapshotFormat, Version: 1, Scope: "full",
		Cards:         []SnapCard{{Key: "c1", Card: cast.Card{Kind: "character", Name: "A", Greetings: []string{"x"}}}, {Key: "p1", Card: cast.Card{Kind: "persona", Name: "P"}}},
		Conversations: []SnapConversation{{Key: "t1", Title: "T", Character: "c1", Persona: "p1", Messages: []SnapMessage{{Role: "user", Content: "hi", Status: "complete"}}}}}
}

func TestSnapshotValidation(t *testing.T) {
	ok := snap(t)
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	mut := map[string]func(*Snapshot){
		"version":        func(s *Snapshot) { s.Version = 3 },
		"format":         func(s *Snapshot) { s.Format = "x" },
		"scope":          func(s *Snapshot) { s.Scope = "all" },
		"dup card key":   func(s *Snapshot) { s.Cards[1].Key = "c1" },
		"dup conv key":   func(s *Snapshot) { s.Conversations = append(s.Conversations, s.Conversations[0]) },
		"wrong kind ref": func(s *Snapshot) { s.Conversations[0].Character = "p1" },
		"missing ref":    func(s *Snapshot) { s.Conversations[0].Persona = "zzz" },
		"bad role":       func(s *Snapshot) { s.Conversations[0].Messages[0].Role = "system" },
		"bad status":     func(s *Snapshot) { s.Conversations[0].Messages[0].Status = "pending" },
		"bad card":       func(s *Snapshot) { s.Cards[0].Card.Name = "" },
		"bad greeting":   func(s *Snapshot) { s.Conversations[0].Greeting = 99 },
		"bad speaker":    func(s *Snapshot) { s.Conversations[0].Messages[0].Speaker = &cast.Identity{Portrait: "http://x/y.png"} },
		"bad color":      func(s *Snapshot) { s.Conversations[0].Messages[0].Speaker = &cast.Identity{TextColor: "red"} },
		"huge message":   func(s *Snapshot) { s.Conversations[0].Messages[0].Content = strings.Repeat("a", maxMessageBytes+1) },
		"default kind":   func(s *Snapshot) { s.Defaults = &SnapDefaults{Character: "p1"} },
	}
	for name, f := range mut {
		s := snap(t)
		f(&s)
		if err := s.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	s := snap(t)
	s.Conversations[0].Messages[0].Status = "streaming"
	if err := s.Validate(); err != nil || s.Conversations[0].Messages[0].Status != "interrupted" {
		t.Fatal("streaming must normalize to interrupted")
	}
}

func TestSnapshotJSONStrictness(t *testing.T) {
	s := snap(t)
	good, _ := json.Marshal(s)
	if _, err := Parse(good); err != nil {
		t.Fatal(err)
	}
	for name, in := range map[string]string{
		"unknown field": strings.Replace(string(good), `"scope"`, `"endpoints":[],"scope"`, 1),
		"trailing":      string(good) + `{}`,
		"future":        strings.Replace(string(good), `"version":1`, `"version":9`, 1),
	} {
		if _, err := Parse([]byte(in)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
