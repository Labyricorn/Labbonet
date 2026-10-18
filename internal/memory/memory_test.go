package memory

import (
	"math"
	"strings"
	"testing"
	"time"
)

const sample = `# selah
tags: angel, divine [0.8], wings [0.5]
relation: gate
use: Describe her light as warm, never harsh.
Selah is an angel who keeps the gate.

# gate
tags: threshold
offscreen_default: true
The gate stands at the edge of the realm.

---
General world prose about the seasons and the long winter that
follows the harvest.

city_tags: market [1.0], bridge
city_relation: nowhere
`

func mustParse(t *testing.T, raw string) *Book {
	t.Helper()
	b, err := ParseStorybook(raw)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseStorybookSectionsFlatDirectivesAndProse(t *testing.T) {
	b := mustParse(t, sample)
	if b.Counts.Entities != 3 || b.Counts.Subtrees != 0 || len(b.ProseChunks) != 1 {
		t.Fatalf("counts=%+v chunks=%d", b.Counts, len(b.ProseChunks))
	}
	selah := b.Entities["selah"]
	if len(selah.Tags) != 3 || selah.Tags[1] != (Tag{"divine", 0.8}) || selah.Tags[0].Weight != 1 {
		t.Fatalf("tags=%+v", selah.Tags)
	}
	if selah.Prose != "Selah is an angel who keeps the gate." || !strings.Contains(selah.UsePrinciples, "warm") {
		t.Fatalf("selah=%+v", selah)
	}
	if !b.Entities["gate"].OffscreenDefault || b.Entities["city"] == nil || len(b.Entities["city"].Tags) != 2 {
		t.Fatalf("entities=%+v", b.Entities)
	}
	if !strings.Contains(b.ProseChunks[0].Text, "long winter") {
		t.Fatalf("chunk=%q", b.ProseChunks[0].Text)
	}
	joined := strings.Join(b.Warnings, "\n")
	if !strings.Contains(joined, `relation "nowhere" matches no entity`) || !strings.Contains(joined, "city: no description text") {
		t.Fatalf("warnings=%v", b.Warnings)
	}
}

func TestParseStorybookSubtreesAndLimits(t *testing.T) {
	b := mustParse(t, "selah.wings_tags: feathers\nselah.wings: x\n# selah.halo\ntags: glow\nA ring of light.")
	if b.Entities["selah.wings"].Kind != "subtree" || b.Entities["selah.halo"].Parent != "selah" || b.Counts.Subtrees != 2 {
		t.Fatalf("book=%+v", b.Counts)
	}
	if _, err := ParseStorybook(strings.Repeat("x", MaxStorybookBytes+1)); err == nil {
		t.Fatal("oversized storybook accepted")
	}
	var many strings.Builder
	for i := 0; i < maxEntities+1; i++ {
		many.WriteString("e" + strings.Repeat("a", i%7) + string(rune('a'+i%26)) + string(rune('a'+(i/26)%26)) + string(rune('a'+(i/676)%26)) + "_tags: x\n")
	}
	if _, err := ParseStorybook(many.String()); err == nil {
		t.Fatal("entity flood accepted")
	}
	if b := mustParse(t, ""); !b.Empty() {
		t.Fatal("empty book not empty")
	}
}

func TestParseTagsToleratesBadWeights(t *testing.T) {
	got := ParseTags("a, b [x], c [0.5], , [2], d [999]")
	want := []Tag{{"a", 1}, {"b [x]", 1}, {"c", 0.5}, {"d", 1}}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %+v want %+v", got, want)
		}
	}
}

func TestContainsWordIsBoundaryAndUnicodeAware(t *testing.T) {
	for _, c := range []struct {
		hay, term string
		want      bool
	}{
		{"the ash fell", "ash", true},
		{"a crash", "ash", false},
		{"ashes", "ash", false},
		{"(ash)", "ash", true},
		{"naïve café", "café", true},
		{"cafés", "café", false},
		{"snake_case", "case", false},
		{"two words here", "words here", true},
		{"", "x", false},
		{"x", "", false},
	} {
		if got := ContainsWord(c.hay, c.term); got != c.want {
			t.Errorf("ContainsWord(%q,%q)=%v want %v", c.hay, c.term, got, c.want)
		}
	}
}

func settings() Settings { s := Defaults(); s.RetrievalBudget = 100000; return s }

func ids(inj []Injection) string {
	var out []string
	for _, i := range inj {
		out = append(out, i.EntityID)
	}
	return strings.Join(out, ",")
}

func TestRetrieveTagFiringExpansionAndSuppression(t *testing.T) {
	b := mustParse(t, sample)
	inj, rec := Retrieve(Input{Book: b, Window: []string{"I saw an angel today"}, Settings: settings(), Free: 100000, Warmth: Warmth{}})
	// selah fires by identity (angel weight 1 >= threshold). gate is offscreen, so the
	// relational hop from selah must not pull it in.
	if ids(inj) != "selah" {
		t.Fatalf("injected %q; record=%+v", ids(inj), rec.Candidates)
	}
	if !strings.Contains(inj[0].Text, "[Render guidance] Describe her light") {
		t.Fatalf("use principles missing: %q", inj[0].Text)
	}
	// Weights sum: two weak tags together reach the threshold (thematic cluster).
	inj, rec = Retrieve(Input{Book: b, Window: []string{"divine wings"}, Settings: settings(), Free: 100000, Warmth: Warmth{}})
	if ids(inj) != "selah" || rec.Candidates[0].Trigger != "thematic_cluster" || rec.Candidates[0].WeightSum != 1.3 {
		t.Fatalf("inj=%q cand=%+v", ids(inj), rec.Candidates)
	}
	// A single weak tag does not fire.
	if inj, _ = Retrieve(Input{Book: b, Window: []string{"just wings"}, Settings: settings(), Free: 100000, Warmth: Warmth{}}); len(inj) != 0 {
		t.Fatalf("weak tag fired: %q", ids(inj))
	}
	// Naming the gate fires it by identity.
	inj, _ = Retrieve(Input{Book: b, Window: []string{"to the threshold"}, Settings: settings(), Free: 100000, Warmth: Warmth{}})
	if ids(inj) != "gate" {
		t.Fatalf("gate not injected: %q", ids(inj))
	}
	// Records hold identifiers and scores, never message text.
	_, rec = Retrieve(Input{Book: b, Window: []string{"secret message text divine"}, Settings: settings(), Free: 100000, Warmth: Warmth{}})
	if len(rec.Shadow) != 2 || strings.Contains(strings.Join(rec.Injected, ""), "secret") || rec.Window.Hash == "" {
		t.Fatalf("shadow=%+v", rec.Shadow)
	}
}

func TestRetrieveOffscreenThematicHitIsSuppressed(t *testing.T) {
	b := mustParse(t, "# hidden\ntags: moon [0.6], tide [0.6]\noffscreen_default: true\nA secret tide.\n")
	inj, rec := Retrieve(Input{Book: b, Window: []string{"the moon and the tide"}, Settings: settings(), Free: 100000, Warmth: Warmth{}})
	if len(inj) != 0 || rec.Candidates[0].Reason != "offscreen_suppressed" {
		t.Fatalf("inj=%q rec=%+v", ids(inj), rec.Candidates)
	}
	// An explicit mention of the entity's own name justifies it.
	inj, _ = Retrieve(Input{Book: b, Window: []string{"moon tide hidden"}, Settings: settings(), Free: 100000, Warmth: Warmth{}})
	if ids(inj) != "hidden" {
		t.Fatalf("named entity suppressed: %q", ids(inj))
	}
}

func TestRetrieveBudgetBoundsAndDedupe(t *testing.T) {
	book := "# a\ntags: alpha\n" + strings.Repeat("A", 300) + "\n# b\ntags: alpha\n" + strings.Repeat("B", 300) + "\n# c\ntags: alpha\n" + strings.Repeat("A", 300) + "\n"
	b := mustParse(t, book)
	s := settings()
	s.RetrievalBudget = Cost(strings.Repeat("A", 300)) + 10
	inj, rec := Retrieve(Input{Book: b, Window: []string{"alpha"}, Settings: s, Free: 100000, Warmth: Warmth{}})
	if len(inj) != 1 || rec.Budget.Spent > rec.Budget.Ceiling {
		t.Fatalf("inj=%q budget=%+v", ids(inj), rec.Budget)
	}
	// Identical text from two entities costs once even when both would fit.
	s.RetrievalBudget = 100000
	inj, rec = Retrieve(Input{Book: b, Window: []string{"alpha"}, Settings: s, Free: 100000, Warmth: Warmth{}})
	if len(inj) != 2 {
		t.Fatalf("dedupe failed: %q %+v", ids(inj), rec.Candidates)
	}
	dup := 0
	for _, c := range rec.Candidates {
		if c.Reason == "duplicate" {
			dup++
		}
	}
	if dup != 1 {
		t.Fatalf("duplicates=%d", dup)
	}
	// Free context caps the ceiling below the configured budget; none or negative injects nothing.
	for _, free := range []int{0, -50} {
		if inj, rec = Retrieve(Input{Book: b, Window: []string{"alpha"}, Settings: s, Free: free, Warmth: Warmth{}}); len(inj) != 0 || rec.Budget.Ceiling != 0 {
			t.Fatalf("free=%d still injected: %q", free, ids(inj))
		}
	}
}

func TestRetrieveWarmthKeepsEntityLitThenDecays(t *testing.T) {
	b := mustParse(t, "# selah\ntags: angel\nAn angel.\n")
	s := settings()
	s.WarmthTurns = 2
	w := Warmth{}
	if inj, _ := Retrieve(Input{Book: b, Window: []string{"angel"}, Settings: s, Free: 9999, Warmth: w}); ids(inj) != "selah" {
		t.Fatal("did not fire")
	}
	for turn := 1; turn <= 2; turn++ {
		inj, rec := Retrieve(Input{Book: b, Window: []string{"nothing relevant"}, Settings: s, Free: 9999, Warmth: w})
		if ids(inj) != "selah" || rec.Candidates[0].Trigger != "warmth" {
			t.Fatalf("turn %d: %q %+v", turn, ids(inj), rec.Candidates)
		}
	}
	if inj, _ := Retrieve(Input{Book: b, Window: []string{"nothing relevant"}, Settings: s, Free: 9999, Warmth: w}); len(inj) != 0 {
		t.Fatalf("warmth did not decay: %q", ids(inj))
	}
	// Zero disables warmth.
	s.WarmthTurns = 0
	w = Warmth{}
	Retrieve(Input{Book: b, Window: []string{"angel"}, Settings: s, Free: 9999, Warmth: w})
	if inj, _ := Retrieve(Input{Book: b, Window: []string{"quiet"}, Settings: s, Free: 9999, Warmth: w}); len(inj) != 0 {
		t.Fatalf("warmth with zero turns: %q", ids(inj))
	}
}

func TestRetrieveSemanticAndFallbackReasons(t *testing.T) {
	b := mustParse(t, "# selah\ntags: angel\nAn angel keeps the gate.\n---\nThe long winter follows harvest.\n")
	s := settings()
	s.RetrieverA = true
	docs := map[string][]float32{"selah": {1, 0}, "doc:0": {0, 1}}
	sem := &Semantic{Available: true, Model: "embed", Query: []float32{0.1, 1}, Docs: docs}
	inj, rec := Retrieve(Input{Book: b, Window: []string{"snow is coming"}, Settings: s, Free: 9999, Warmth: Warmth{}, Semantic: sem, Now: time.Now()})
	if ids(inj) != "doc:0" || !rec.Semantic.Available || rec.Semantic.DocsScored != 2 || rec.Candidates[0].Trigger != "semantic" {
		t.Fatalf("inj=%q rec=%+v", ids(inj), rec.Semantic)
	}
	// Below the floor nothing is taken.
	s.SemanticFloor = 0.999
	if inj, _ = Retrieve(Input{Book: b, Window: []string{"x"}, Settings: s, Free: 9999, Warmth: Warmth{}, Semantic: sem}); len(inj) != 0 {
		t.Fatalf("below floor: %q", ids(inj))
	}
	// Unavailable embeddings degrade to tags only, with the reason recorded.
	s.SemanticFloor = 0.25
	for _, sm := range []*Semantic{nil, {Available: false, Reason: "embedding_model_not_loaded"}, {Available: true, Query: []float32{float32(math.NaN())}}} {
		inj, rec = Retrieve(Input{Book: b, Window: []string{"an angel"}, Settings: s, Free: 9999, Warmth: Warmth{}, Semantic: sm})
		if ids(inj) != "selah" || rec.Semantic.Available || rec.Semantic.Reason == "" {
			t.Fatalf("fallback failed: %q %+v", ids(inj), rec.Semantic)
		}
	}
	s.RetrievalEnabled = false
	if inj, rec = Retrieve(Input{Book: b, Window: []string{"an angel"}, Settings: s, Free: 9999, Warmth: Warmth{}}); len(inj) != 0 || rec.Reason == "" {
		t.Fatalf("disabled still ran: %q", ids(inj))
	}
}

func TestVectorsRoundTripAndRejectCorruption(t *testing.T) {
	v := []float32{0.5, -1, 3.25}
	got, err := DecodeVector(EncodeVector(v), 3)
	if err != nil || got[1] != -1 || got[2] != 3.25 {
		t.Fatalf("got=%v err=%v", got, err)
	}
	if _, err = DecodeVector(EncodeVector(v), 4); err == nil {
		t.Fatal("wrong dim accepted")
	}
	nan := EncodeVector([]float32{float32(math.NaN())})
	if _, err = DecodeVector(nan, 1); err == nil {
		t.Fatal("NaN accepted")
	}
	if Cosine([]float32{1, 0}, []float32{1, 0}) < 0.999 || Cosine([]float32{1, 0}, []float32{0, 1}) != 0 || Cosine([]float32{1}, []float32{1, 2}) != 0 || Cosine([]float32{0, 0}, []float32{1, 1}) != 0 {
		t.Fatal("cosine")
	}
	if Hash("a") == Hash("b") || len(Hash("a")) != 64 {
		t.Fatal("hash")
	}
}

func TestSettingsValidation(t *testing.T) {
	if err := Defaults().Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mut := range map[string]func(*Settings){
		"nan threshold":     func(s *Settings) { s.FireThreshold = math.NaN() },
		"huge budget":       func(s *Settings) { s.RetrievalBudget = 1 << 30 },
		"depth":             func(s *Settings) { s.ExpansionDepth = 2 },
		"window":            func(s *Settings) { s.WindowMessages = 0 },
		"summary tiny":      func(s *Settings) { s.SummaryMaxChars = 10 },
		"half compression":  func(s *Settings) { s.CompressionModel = "m" },
		"newline prefix":    func(s *Settings) { s.EmbedDocPrefix = "a\nb" },
		"semantic no model": func(s *Settings) { s.RetrieverA = true },
	} {
		s := Defaults()
		mut(&s)
		if err := s.Validate(); err == nil || !IsSettingError(err) {
			t.Errorf("%s accepted: %v", name, err)
		}
	}
}
