package memory

import (
	"math"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Warmth keeps an entity lit for a few turns after it fires so it does not flicker in and
// out of context. It is session-scoped runtime state, never persisted.
type Warmth map[string]int

// Semantic carries the embedding results the caller gathered (the engine does no I/O).
// Available=false means the turn proceeds tag-only, with Reason shown in the record.
type Semantic struct {
	Available     bool
	Reason        string
	Model         string
	Query         []float32
	Docs          map[string][]float32 // keyed by Doc.ID
	NewEmbeddings int
	PendingDocs   int // docs left unembedded this turn because of the per-turn cap
}

// Input is everything one retrieval decision depends on.
type Input struct {
	Book     *Book
	Window   []string // recent message texts, oldest first
	Settings Settings
	// Free is the headroom (in Cost units) left in the context after everything that
	// cannot be dropped; retrieval never spends more than this nor Settings.RetrievalBudget.
	Free     int
	Warmth   Warmth
	Semantic *Semantic
	Now      time.Time
}

// Injection is text chosen for the prompt.
type Injection struct {
	EntityID string `json:"entity_id"`
	Text     string `json:"text"`
	Cost     int    `json:"cost"`
}

// CandidateRecord documents one considered candidate, included or not.
type CandidateRecord struct {
	ID          string  `json:"id"`
	Kind        string  `json:"kind"`
	Parent      string  `json:"parent,omitempty"`
	Trigger     string  `json:"trigger"`
	Matched     []Tag   `json:"matched_tags,omitempty"`
	WeightSum   float64 `json:"weight_sum"`
	Similarity  float64 `json:"similarity,omitempty"`
	Score       float64 `json:"score"`
	Offscreen   bool    `json:"offscreen_default,omitempty"`
	Included    bool    `json:"included"`
	Reason      string  `json:"reason,omitempty"` // why it was not included
	UseAttached bool    `json:"use_attached,omitempty"`
	Cost        int     `json:"cost"`
}

// ShadowRecord is a counterfactual: what a different fire threshold would have fired.
type ShadowRecord struct {
	FireThreshold float64  `json:"fire_threshold"`
	WouldFire     []string `json:"would_fire"`
}

// Record is the per-generation retrieval telemetry shown in the inspector. It holds
// identifiers, scores and costs only: never message or storybook text.
type Record struct {
	At           time.Time         `json:"at"`
	Enabled      bool              `json:"enabled"`
	Reason       string            `json:"reason,omitempty"` // why nothing ran
	RetrieverA   bool              `json:"retriever_a"`
	RetrieverB   bool              `json:"retriever_b"`
	FireThresh   float64           `json:"fire_threshold"`
	Window       WindowRecord      `json:"window"`
	Semantic     SemanticRecord    `json:"semantic"`
	Budget       BudgetRecord      `json:"budget"`
	Candidates   []CandidateRecord `json:"candidates"`
	Shadow       []ShadowRecord    `json:"shadow,omitempty"`
	Injected     []string          `json:"injected"`
	Counts       Counts            `json:"storybook"`
	LatencyMS    float64           `json:"latency_ms"`
	ContextNotes []string          `json:"context_notes,omitempty"`
	Context      ContextRecord     `json:"context"`
}

type WindowRecord struct {
	Messages int    `json:"messages"`
	Cost     int    `json:"cost"`
	Hash     string `json:"hash"`
}

type SemanticRecord struct {
	Requested     bool   `json:"requested"`
	Available     bool   `json:"available"`
	Reason        string `json:"reason,omitempty"`
	Model         string `json:"model,omitempty"`
	DocsScored    int    `json:"docs_scored"`
	NewEmbeddings int    `json:"new_embeddings"`
	PendingDocs   int    `json:"pending_docs"`
}

type BudgetRecord struct {
	Setting int `json:"setting"`
	Free    int `json:"free"`
	Ceiling int `json:"ceiling"`
	Spent   int `json:"spent"`
}

type candidate struct {
	id        string
	kind      string
	parent    string
	triggers  map[string]bool
	matched   []Tag
	weightSum float64
	sim       float64
	offscreen bool
	role      string
	use       string
	prose     string
	nameHit   bool
}

// isWordRune is the boundary test for tag matching (Unicode-aware, unlike regexp \b).
func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }

// ContainsWord reports whether term occurs in lower (already lowercased) delimited by
// non-word characters, so "ash" does not fire on "crash".
func ContainsWord(lower, term string) bool {
	term = strings.ToLower(strings.TrimSpace(term))
	if term == "" {
		return false
	}
	for from := 0; from <= len(lower)-len(term); {
		i := strings.Index(lower[from:], term)
		if i < 0 {
			return false
		}
		start := from + i
		end := start + len(term)
		before, after := true, true
		if start > 0 {
			r, _ := utf8.DecodeLastRuneInString(lower[:start])
			before = !isWordRune(r)
		}
		if end < len(lower) {
			r, _ := utf8.DecodeRuneInString(lower[end:])
			after = !isWordRune(r)
		}
		if before && after {
			return true
		}
		from = start + 1
		for from < len(lower) && !utf8.RuneStart(lower[from]) {
			from++
		}
	}
	return false
}

func round(v float64, places int) float64 {
	p := math.Pow(10, float64(places))
	return math.Round(v*p) / p
}

// Retrieve chooses storybook text to inject. It never fails: any problem degrades to
// injecting less, with the reason in the record.
func Retrieve(in Input) ([]Injection, Record) {
	started := time.Now()
	s := in.Settings
	rec := Record{At: in.Now, Enabled: s.RetrievalEnabled, RetrieverA: s.RetrieverA, RetrieverB: s.RetrieverB,
		FireThresh: s.FireThreshold, Candidates: []CandidateRecord{}, Injected: []string{}}
	rec.Budget.Setting, rec.Budget.Free = s.RetrievalBudget, in.Free
	finish := func(inj []Injection) ([]Injection, Record) {
		rec.LatencyMS = round(float64(time.Since(started))/float64(time.Millisecond), 2)
		return inj, rec
	}
	if in.Book != nil {
		rec.Counts = in.Book.Counts
	}
	if !s.RetrievalEnabled {
		rec.Reason = "retrieval disabled"
		return finish(nil)
	}
	if in.Book.Empty() {
		rec.Reason = "no storybook authored for this character"
		return finish(nil)
	}
	book := in.Book
	window := strings.Join(in.Window, "\n")
	lower := strings.ToLower(window)
	rec.Window = WindowRecord{Messages: len(in.Window), Cost: Cost(window), Hash: Hash(window)[:16]}

	cands := map[string]*candidate{}
	touch := func(id string) *candidate {
		if c := cands[id]; c != nil {
			return c
		}
		c := &candidate{id: id, kind: "doc", triggers: map[string]bool{}}
		if e := book.Entities[id]; e != nil {
			c.kind, c.parent, c.offscreen, c.role, c.use, c.prose = e.Kind, e.Parent, e.OffscreenDefault, e.RoleInArcs, e.UsePrinciples, e.Prose
		}
		cands[id] = c
		return c
	}

	// Retriever B: weighted tags. An entity fires when matched weight reaches the threshold.
	sums := map[string]float64{}
	if s.RetrieverB {
		for _, id := range book.Order {
			e := book.Entities[id]
			if len(e.Tags) == 0 {
				continue
			}
			var sum float64
			var matched []Tag
			identity := false
			for _, t := range e.Tags {
				if ContainsWord(lower, t.Term) {
					matched = append(matched, t)
					sum += t.Weight
					if t.Weight >= s.FireThreshold {
						identity = true
					}
				}
			}
			sums[id] = sum
			if sum >= s.FireThreshold {
				c := touch(id)
				c.matched, c.weightSum = matched, sum
				c.nameHit = ContainsWord(lower, id[strings.LastIndex(id, ".")+1:])
				if identity {
					c.triggers["identity"] = true
				} else {
					c.triggers["thematic_cluster"] = true
				}
			}
		}
	}

	// Retriever A: cosine similarity of the recent window against cached document vectors.
	rec.Semantic.Requested = s.RetrieverA
	if s.RetrieverA {
		sem := in.Semantic
		switch {
		case sem == nil:
			rec.Semantic.Reason = "semantic retrieval unavailable"
		case !sem.Available || !ValidVector(sem.Query):
			rec.Semantic.Reason = sem.Reason
			if rec.Semantic.Reason == "" {
				rec.Semantic.Reason = "no query vector"
			}
			rec.Semantic.Model = sem.Model
		default:
			rec.Semantic.Available, rec.Semantic.Model = true, sem.Model
			rec.Semantic.NewEmbeddings, rec.Semantic.PendingDocs = sem.NewEmbeddings, sem.PendingDocs
			type scored struct {
				doc Doc
				sim float64
			}
			var all []scored
			for _, d := range book.Docs() {
				v, ok := sem.Docs[d.ID]
				if !ok {
					continue
				}
				rec.Semantic.DocsScored++
				all = append(all, scored{d, Cosine(sem.Query, v)})
			}
			sort.SliceStable(all, func(i, j int) bool { return all[i].sim > all[j].sim })
			for i, sc := range all {
				if i >= s.TopKA || sc.sim < s.SemanticFloor {
					break
				}
				target := sc.doc.EntityID
				if target == "" {
					target = sc.doc.ID
				}
				c := touch(target)
				c.sim = math.Max(c.sim, sc.sim)
				c.triggers["semantic"] = true
				if sc.doc.EntityID == "" {
					c.prose = sc.doc.Text
				}
			}
		}
	}

	// One relational hop from entities that fired by tags; offscreen entities are not pulled.
	if s.ExpansionDepth >= 1 && s.RetrieverB {
		var fired []string
		for id, c := range cands {
			if c.triggers["identity"] || c.triggers["thematic_cluster"] {
				fired = append(fired, id)
			}
		}
		sort.Strings(fired)
		for _, id := range fired {
			for _, rel := range book.Entities[id].Relations {
				nid := book.resolve(rel)
				if nid == "" || book.Entities[nid].OffscreenDefault || cands[nid] != nil {
					continue
				}
				touch(nid).triggers["relational"] = true
			}
		}
	}

	// Warmth: age, then surface anything still warm that did not fire this turn.
	if in.Warmth == nil {
		in.Warmth = Warmth{}
	}
	for id := range in.Warmth {
		in.Warmth[id]--
		if in.Warmth[id] <= 0 {
			delete(in.Warmth, id)
		}
	}
	warm := make([]string, 0, len(in.Warmth))
	for id := range in.Warmth {
		warm = append(warm, id)
	}
	sort.Strings(warm)
	for _, id := range warm {
		if cands[id] == nil && book.Entities[id] != nil {
			touch(id).triggers["warmth"] = true
		}
	}

	type ranked struct {
		c          *candidate
		trigger    string
		score      float64
		suppressed bool
	}
	var list []ranked
	ids := make([]string, 0, len(cands))
	for id := range cands {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		c := cands[id]
		tt := "manual"
		for _, name := range []string{"identity", "thematic_cluster", "relational", "semantic", "warmth"} {
			if c.triggers[name] {
				tt = name
				break
			}
		}
		wScore := 0.0
		if c.weightSum > 0 {
			wScore = math.Min(1, c.weightSum/(s.FireThreshold*2))
		}
		base := math.Max(wScore, c.sim)
		if tt == "warmth" || tt == "relational" {
			base *= 0.6
		}
		if strings.TrimSpace(c.role) != "" {
			base *= 1.15
		}
		r := ranked{c: c, trigger: tt, score: round(base, 4)}
		// A thematic-only hit on an offscreen entity is vetoed unless an identity tag, an
		// explicit name mention or a fired subtree justifies it.
		if c.offscreen && c.kind == "entity" && c.triggers["thematic_cluster"] && !c.triggers["identity"] && !c.nameHit {
			subtree := false
			for _, o := range cands {
				if o.parent == id && (o.triggers["identity"] || o.triggers["thematic_cluster"]) {
					subtree = true
				}
			}
			r.suppressed = !subtree
		}
		list = append(list, r)
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].score > list[j].score })

	ceiling := s.RetrievalBudget
	if in.Free < ceiling {
		ceiling = in.Free
	}
	if ceiling < 0 {
		ceiling = 0
	}
	rec.Budget.Ceiling = ceiling
	var out []Injection
	seen := map[string]bool{}
	for _, r := range list {
		c := r.c
		row := CandidateRecord{ID: c.id, Kind: c.kind, Parent: c.parent, Trigger: r.trigger, Matched: c.matched,
			WeightSum: round(c.weightSum, 3), Similarity: round(c.sim, 4), Score: r.score, Offscreen: c.offscreen, UseAttached: strings.TrimSpace(c.use) != ""}
		text := strings.TrimSpace(c.prose)
		if c.use != "" {
			if text != "" {
				text += "\n"
			}
			text += "[Render guidance] " + strings.TrimSpace(c.use)
		}
		if text == "" {
			row.Reason = "no_payload"
			rec.Candidates = append(rec.Candidates, row)
			continue
		}
		row.Cost = Cost(text)
		switch {
		case r.suppressed:
			row.Reason = "offscreen_suppressed"
		case seen[Hash(text)]:
			row.Reason = "duplicate"
		case rec.Budget.Spent+row.Cost > ceiling:
			row.Reason = "budget"
		default:
			row.Included = true
			rec.Budget.Spent += row.Cost
			seen[Hash(text)] = true
			out = append(out, Injection{EntityID: c.id, Text: text, Cost: row.Cost})
			rec.Injected = append(rec.Injected, c.id)
			if (r.trigger == "identity" || r.trigger == "thematic_cluster" || r.trigger == "semantic") && s.WarmthTurns > 0 {
				in.Warmth[c.id] = s.WarmthTurns + 1 // aged once at the start of the next turn
			}
		}
		rec.Candidates = append(rec.Candidates, row)
	}
	if s.RetrieverB {
		for _, f := range []float64{s.FireThreshold * 0.5, s.FireThreshold * 1.5} {
			sh := ShadowRecord{FireThreshold: round(f, 2), WouldFire: []string{}}
			for _, id := range book.Order {
				if sum, ok := sums[id]; ok && sum >= f {
					sh.WouldFire = append(sh.WouldFire, id)
				}
			}
			rec.Shadow = append(rec.Shadow, sh)
		}
	}
	return finish(out)
}

// ContextRecord describes how the prompt was fitted for this generation.
type ContextRecord struct {
	Limit            int   `json:"limit"`
	Estimated        int   `json:"estimated"`
	SummarySeq       int64 `json:"summary_seq,omitempty"`
	ArchivedMessages int   `json:"archived_messages,omitempty"`
	OmittedMessages  int   `json:"omitted_messages,omitempty"`
	RetrievedCost    int   `json:"retrieved_cost,omitempty"`
}
