package generate

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Labyricorn/Labbonet/internal/cast"
	"github.com/Labyricorn/Labbonet/internal/memory"
	"github.com/Labyricorn/Labbonet/internal/prompt"
	"github.com/Labyricorn/Labbonet/internal/providers"
	"github.com/Labyricorn/Labbonet/internal/providers/openai"
	"github.com/Labyricorn/Labbonet/internal/store"
)

// Original implementation: Copyright (C) 2026 Labbonet contributors. AGPL-3.0-or-later.
// Behavior reference: GobboNet 07-prompt.js buildContextMessages/summarizeForLore and
// 08-rag.js @ 314bdc9cae204ae38fdc1002ac6a9fb1437c109f (Copyright (C) 2026 Elodine /
// GoblinCorps): trigger/landing/reserve fractions, "a worse outcome beats losing
// messages" fallbacks. No upstream source copied; see docs/REUSE-LOG.md.

const (
	budgetFactor         = 90 // percent of the context window usable as input
	responseReservePct   = 20 // of that budget, kept free for the reply when deciding to compress
	recentReservePct     = 22 // most recent slice of the conversation always kept verbatim
	landingPct           = 65 // target fullness after a compression pass
	summaryRetryBackoff  = 5 * time.Minute
	summaryTimeout       = 90 * time.Second
	embedTimeout         = 20 * time.Second
	embedDocsPerTurn     = 31 // one request: the query plus up to this many uncached documents
	maxTranscriptMessage = 4000
	maxQueryBytes        = 4000
)

var (
	errSummaryOversize = errors.New("summary is far longer than the limit")
	thinkBlock         = regexp.MustCompile(`(?s)<think>.*?</think>`)
	summaryLabel       = regexp.MustCompile(`(?i)^(updated )?(summary|beat|note|entry|output)\s*[:\-]\s*`)
)

// memoryState is per-Runner runtime state for retrieval warmth; it is session-scoped
// flicker control and deliberately not persisted.
type memoryState struct {
	mu     sync.Mutex
	warmth map[int64]memory.Warmth
}

// warmthCopy returns a private working copy of the conversation's committed warmth.
// Retrieval ages and extends the copy; nothing reaches shared state until commitWarmth, so
// overlapping preparations cannot race on the map and a turn that is never stored (conflict,
// cancellation, failed assembly) neither ages nor adds warmth.
func (rn *Runner) warmthCopy(convID int64) memory.Warmth {
	rn.mem.mu.Lock()
	defer rn.mem.mu.Unlock()
	out := make(memory.Warmth, len(rn.mem.warmth[convID]))
	for id, n := range rn.mem.warmth[convID] {
		out[id] = n
	}
	return out
}

// commitWarmth publishes the working copy once the turn it was computed for is stored. When
// several accepted turns overlap, the last to commit wins.
func (rn *Runner) commitWarmth(convID int64, w memory.Warmth) {
	rn.mem.mu.Lock()
	defer rn.mem.mu.Unlock()
	if rn.mem.warmth == nil {
		rn.mem.warmth = map[int64]memory.Warmth{}
	}
	rn.mem.warmth[convID] = w
}

type memoryInput struct {
	Conv      *store.Conversation
	Endpoint  *store.Endpoint
	ModelID   string
	Character *cast.Card
	Persona   *cast.Card
	Path      []store.PathMessage // the visible branch before the pending user message
	// PendingID is the stored user message a regeneration answers. It is not in Path (it is the
	// pending message) but a summary anchored there still belongs to this branch.
	PendingID    *int64
	User         string
	Turn         int
	ContextLimit int
	MaxTokens    int
	Now          time.Time
	// NoCompress skips the (model-calling) compression step; used for dry runs.
	NoCompress bool
}

// memoryRun is what prompt assembly needs from the memory system, plus the retrieval
// record to store once the generation row exists.
type memoryRun struct {
	History    []providers.Message
	Summary    string
	SummarySeq int64
	Archived   int
	Retrieve   func(headroom int) []memory.Injection
	Notes      []string
	Record     *memory.Record
	Keep       bool // worth storing: a storybook, semantic request or summary activity

	// WarmthNext is the working warmth Retrieve produced; commit it with CommitWarmth only
	// after the turn is stored. ConvID names the conversation it belongs to.
	WarmthNext memory.Warmth
	ConvID     int64
	commit     func(convID int64, w memory.Warmth)
}

// CommitWarmth publishes the retrieval warmth for an accepted turn. It is a no-op when
// retrieval did not run.
func (m *memoryRun) CommitWarmth() {
	if m.WarmthNext != nil && m.commit != nil {
		m.commit(m.ConvID, m.WarmthNext)
	}
}

// planArchive decides how many of the oldest live messages to replace by a summary.
// costs are memory.Cost values for live; isUser marks user turns so the cut falls on a
// turn boundary. It returns 0 when the conversation still fits with room for a reply.
func planArchive(limit, fixed, summaryCost int, costs []int, isUser []bool) int {
	budget := limit * budgetFactor / 100
	effective := budget - budget*responseReservePct/100
	live := 0
	for _, c := range costs {
		live += c
	}
	if len(costs) < 2 || fixed+summaryCost+live <= effective {
		return 0
	}
	recentReserve := budget * recentReservePct / 100
	tail, first := 0, len(costs)
	for i := len(costs) - 1; i >= 0; i-- {
		if tail+costs[i] > recentReserve && i < len(costs)-1 {
			first = i + 1
			break
		}
		tail += costs[i]
		first = i
	}
	reserve := summaryCost
	if reserve < 1024 {
		reserve = 1024
	}
	targetLive := budget*landingPct/100 - fixed - reserve
	if targetLive < 0 {
		targetLive = 0
	}
	toFree := live - targetLive
	n, freed := 0, 0
	for n < first && freed < toFree {
		freed += costs[n]
		n++
	}
	for n > 0 && n < len(isUser) && !isUser[n] {
		n--
	}
	return n
}

func (rn *Runner) modelInfo(ctx context.Context, endpointID int64, modelID string) *store.Model {
	models, err := rn.Store.ListModels(ctx, endpointID)
	if err != nil {
		return nil
	}
	for _, m := range models {
		if m.ModelID == modelID {
			return m
		}
	}
	return nil
}

// prepareMemory resolves the summary boundary, compresses older messages when the
// conversation no longer fits, and prepares retrieval. Only a cancelled ctx is an error;
// every other failure degrades to "less memory" with a note, never to lost messages.
func (rn *Runner) prepareMemory(ctx context.Context, in memoryInput) (*memoryRun, error) {
	out := &memoryRun{}
	settings, err := rn.Store.GetMemorySettings(ctx)
	if err != nil {
		settings = memory.Defaults()
		out.Notes = append(out.Notes, "memory settings unreadable; defaults used")
	}
	onPath := make(map[int64]int, len(in.Path))
	for i, m := range in.Path {
		onPath[m.ID] = i
	}
	if in.PendingID != nil {
		onPath[*in.PendingID] = len(in.Path)
	}
	eff, err := rn.Store.EffectiveSummary(ctx, in.Conv.ID, onPath)
	if err != nil {
		eff = nil
		out.Notes = append(out.Notes, "summary unreadable; full history used")
	}
	boundary := 0
	if eff != nil {
		out.Summary, out.SummarySeq = eff.Text, eff.Seq
		if eff.ThroughMessageID != nil {
			boundary = onPath[*eff.ThroughMessageID] + 1
		}
	}
	toMsgs := func(from int) []providers.Message {
		msgs := make([]providers.Message, 0, len(in.Path)-from)
		for _, m := range in.Path[from:] {
			msgs = append(msgs, providers.Message{Role: m.Role, Content: m.Content})
		}
		return msgs
	}

	if settings.SummarizeEnabled && !in.NoCompress {
		if err := rn.compressIfNeeded(ctx, in, settings, boundary, out); err != nil {
			return nil, err
		}
		// A pass may have appended a revision; re-resolve against the stored state.
		if eff, err = rn.Store.EffectiveSummary(ctx, in.Conv.ID, onPath); err == nil && eff != nil {
			out.Summary, out.SummarySeq, boundary = eff.Text, eff.Seq, 0
			if eff.ThroughMessageID != nil {
				boundary = onPath[*eff.ThroughMessageID] + 1
			}
		}
	}
	out.Archived = boundary
	out.History = toMsgs(boundary)

	rn.prepareRetrieval(ctx, in, settings, out)
	return out, nil
}

func (rn *Runner) compressIfNeeded(ctx context.Context, in memoryInput, settings memory.Settings, boundary int, out *memoryRun) error {
	live := in.Path[boundary:]
	fixedSnap, err := prompt.Assemble(prompt.Input{Character: in.Character, Persona: in.Persona, System: in.Conv.SystemPrompt, User: in.User, Turn: in.Turn, ContextLimit: in.ContextLimit, MaxTokens: in.MaxTokens, Now: in.Now})
	if err != nil {
		return nil // the real assembly reports the problem
	}
	limit := fixedSnap.ContextLimit
	summaryCost := 0
	if out.Summary != "" {
		summaryCost = memory.Cost("[Summary of earlier conversation]\n" + out.Summary)
	}
	costs := make([]int, len(live))
	isUser := make([]bool, len(live))
	for i, m := range live {
		costs[i], isUser[i] = memory.Cost(m.Content), m.Role == "user"
	}
	n := planArchive(limit, fixedSnap.EstimatedTokens, summaryCost, costs, isUser)
	if n == 0 {
		return nil
	}
	latest, err := rn.Store.ListSummaries(ctx, in.Conv.ID, 1)
	if err == nil && len(latest) == 1 && latest[0].Source == "failed" && time.Since(latest[0].CreatedAt) < summaryRetryBackoff {
		out.Notes = append(out.Notes, "automatic summary paused for a few minutes after a failure; oldest messages are omitted from the prompt without a summary")
		return nil
	}
	res, err := rn.foldSummary(ctx, in, settings, out.Summary, live, n, boundary)
	if err != nil {
		return err
	}
	out.Notes = append(out.Notes, res.Notes...)
	return nil
}

// foldResult is the outcome of one summary pass: the stored revision (an auto summary or
// a recorded failure; nil if nothing could be stored) and what to tell the user.
type foldResult struct {
	Row    *store.Summary
	Failed bool
	Notes  []string
}

// foldSummary replaces live[:n] by a summary. The messages themselves are never touched:
// the result is a new summary revision whose archive boundary is the last message it
// covers. A manual edit that lands while the model works wins (the revision check fails
// and this result is discarded). The only error returned is a cancelled ctx.
func (rn *Runner) foldSummary(ctx context.Context, in memoryInput, settings memory.Settings, existing string, live []store.PathMessage, n, boundary int) (foldResult, error) {
	var res foldResult
	seq, err := rn.Store.LatestSummarySeq(ctx, in.Conv.ID)
	if err != nil {
		return res, nil
	}
	fed, text, used, runErr := rn.summarizeWithFallback(ctx, in, settings, existing, live[:n])
	if ctx.Err() != nil {
		return res, ctx.Err()
	}
	record := store.Summary{ConversationID: in.Conv.ID, Source: "auto", EndpointID: used.endpointID, Model: used.modelID}
	if runErr != nil {
		res.Failed = true
		record.Source, record.Text = "failed", ""
		record.Note = fmt.Sprintf("%d earlier message(s) could not be summarised (%v); they stay in the conversation but are omitted from the prompt when it is full", n, runErr)
		res.Notes = append(res.Notes, "summary failed: "+runErr.Error())
	} else {
		last := live[fed-1].ID
		record.Text, record.ThroughMessageID, record.AnchorMessageID = text, &last, &last
		record.ArchivedCount = boundary + fed
		res.Notes = append(res.Notes, fmt.Sprintf("summarised %d earlier message(s) with %s", fed, used.modelID))
	}
	row, err := persistedSummary(func(c context.Context) (*store.Summary, error) { return rn.Store.AddSummary(c, record, seq) })
	switch {
	case err == nil:
		res.Row = row
	case errors.Is(err, store.ErrConflict):
		res.Notes = append(res.Notes, "summary was edited during the pass; the edit was kept and this result discarded")
	default:
		res.Notes = append(res.Notes, "summary could not be saved: "+err.Error())
	}
	return res, nil
}

// ErrNothingToSummarise is returned by SummarizeNow when fewer than two exchanges are live.
var ErrNothingToSummarise = errors.New("not enough conversation to summarise yet")

// SummarizeNow is the explicit "summarise now" action: everything before the latest user
// turn is folded into the summary, whether or not the context is full. It needs no
// automatic-summary setting but uses the same model rules (never a not-loaded model).
func (rn *Runner) SummarizeNow(ctx context.Context, convID int64) (*store.Summary, []string, error) {
	conv, err := rn.Store.GetConversation(ctx, convID)
	if err != nil {
		return nil, nil, err
	}
	if conv.EndpointID == nil || conv.ModelID == nil {
		return nil, nil, ErrNoModelSelected
	}
	ep, err := rn.Store.GetEndpoint(ctx, *conv.EndpointID)
	if err != nil {
		return nil, nil, err
	}
	if !ep.Enabled {
		return nil, nil, ErrEndpointDisabled
	}
	_, character, persona, err := rn.Store.ReadCast(ctx, convID)
	if err != nil {
		return nil, nil, err
	}
	path, _, err := rn.Store.ActivePath(ctx, convID)
	if err != nil {
		return nil, nil, err
	}
	onPath := make(map[int64]int, len(path))
	for i, m := range path {
		onPath[m.ID] = i
		if m.Status == "streaming" {
			return nil, nil, store.ErrBusy
		}
	}
	existing, boundary := "", 0
	if eff, err := rn.Store.EffectiveSummary(ctx, convID, onPath); err == nil && eff != nil {
		existing = eff.Text
		if eff.ThroughMessageID != nil {
			boundary = onPath[*eff.ThroughMessageID] + 1
		}
	}
	live := path[boundary:]
	n := -1
	for i := len(live) - 1; i >= 0; i-- {
		if live[i].Role == "user" {
			n = i
			break
		}
	}
	if n < 1 {
		return nil, nil, ErrNothingToSummarise
	}
	settings, err := rn.Store.GetMemorySettings(ctx)
	if err != nil {
		return nil, nil, err
	}
	in := memoryInput{Conv: conv, Endpoint: ep, ModelID: *conv.ModelID, Character: character, Persona: persona, Path: path, Now: time.Now()}
	res, err := rn.foldSummary(ctx, in, settings, existing, live, n, boundary)
	if err != nil {
		return nil, nil, err
	}
	return res.Row, res.Notes, nil
}

func persistedSummary(fn func(context.Context) (*store.Summary, error)) (*store.Summary, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return fn(ctx)
}

type usedModel struct {
	endpointID *int64
	modelID    string
}

// summarizeWithFallback tries the configured compression model, then the chat model. A
// not-loaded model is never called: on LM Studio that would load it and could evict
// another client's model.
func (rn *Runner) summarizeWithFallback(ctx context.Context, in memoryInput, s memory.Settings, existing string, msgs []store.PathMessage) (fed int, text string, used usedModel, err error) {
	type choice struct {
		ep    *store.Endpoint
		model string
	}
	var choices []choice
	if s.CompressionEndpointID != nil {
		if ep, e := rn.Store.GetEndpoint(ctx, *s.CompressionEndpointID); e == nil && ep.Enabled {
			choices = append(choices, choice{ep, s.CompressionModel})
		}
	}
	if (len(choices) == 0 || choices[0].ep.ID != in.Endpoint.ID || choices[0].model != in.ModelID) && in.Endpoint.Enabled {
		choices = append(choices, choice{in.Endpoint, in.ModelID})
	}
	var firstErr error
	for _, c := range choices {
		info := rn.modelInfo(ctx, c.ep.ID, c.model)
		if info != nil && info.State == "not-loaded" {
			if firstErr == nil {
				firstErr = fmt.Errorf("compression model %s is not loaded", c.model)
			}
			continue
		}
		ctxLen := 0
		if info != nil {
			ctxLen = info.ContextLength
		}
		fed, text, err = rn.summarize(ctx, in, c.ep, c.model, ctxLen, s.SummaryMaxChars, existing, msgs)
		if err == nil {
			id := c.ep.ID
			return fed, text, usedModel{&id, c.model}, nil
		}
		if ctx.Err() != nil {
			return 0, "", usedModel{}, ctx.Err()
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	if firstErr == nil {
		firstErr = errors.New("no usable compression model")
	}
	return 0, "", usedModel{}, firstErr
}

func speakerName(c *cast.Card, fallback string) string {
	if c != nil && c.Name != "" {
		return c.Name
	}
	return fallback
}

func (rn *Runner) summarize(ctx context.Context, in memoryInput, ep *store.Endpoint, modelID string, modelCtx, maxChars int, existing string, msgs []store.PathMessage) (int, string, error) {
	if modelCtx <= 0 {
		modelCtx = 8192
	}
	maxTokens := maxChars / 2
	if maxTokens < 256 {
		maxTokens = 256
	}
	if maxTokens > 4096 {
		maxTokens = 4096
	}
	// Instructions (~900) and the reply come out of the compression model's window; the
	// rest, less a 10% margin, is what the transcript and existing summary may use.
	feedBudget := (modelCtx-maxTokens-900)*9/10 - memory.Cost(existing)
	if feedBudget < 500 {
		return 0, "", errors.New("no room to summarise: the compression model's context is too small")
	}
	user, assistant := speakerName(in.Persona, "User"), speakerName(in.Character, "Assistant")
	var transcript strings.Builder
	fed := 0
	for _, m := range msgs {
		name := user
		if m.Role == "assistant" {
			name = assistant
		}
		line := name + ": " + clipText(m.Content, maxTranscriptMessage) + "\n\n"
		if memory.Cost(line) > feedBudget && fed > 0 {
			break
		}
		if memory.Cost(line) > feedBudget {
			line = clipText(line, feedBudget-16)
		}
		feedBudget -= memory.Cost(line)
		transcript.WriteString(line)
		fed++
	}
	if fed == 0 {
		return 0, "", errors.New("nothing to summarise")
	}
	system := fmt.Sprintf("You keep a running summary of a conversation between %s and %s. Reply with ONLY the updated summary: plain prose, no preamble, no headings, no commentary. Keep every fact that still matters (names, relationships, decisions, promises, places, unresolved threads). Merge the new conversation into the existing summary and compress older detail as needed; the summary may shrink. Write in the past tense, third person. Never exceed %d characters.", user, assistant, maxChars)
	body := "[Existing summary]\n" + strings.TrimSpace(existing) + "\n\n[New conversation to fold in]\n" + transcript.String() + "Write the updated summary now."
	if strings.TrimSpace(existing) == "" {
		body = "[Existing summary]\n(none yet)\n\n[New conversation to fold in]\n" + transcript.String() + "Write the summary now."
	}
	target, err := rn.targetFor(ctx, ep)
	if err != nil {
		return 0, "", err
	}
	runCtx, cancel := context.WithTimeout(ctx, summaryTimeout)
	defer cancel()
	release, err := rn.Slots.Acquire(runCtx, ep.ID, ep.ConcurrencyLimit)
	if err != nil {
		return 0, "", err
	}
	defer release()
	temp := 0.3
	var got strings.Builder
	_, err = openai.New(rn.Egress, target).StreamChat(runCtx, providers.ChatRequest{
		Model: modelID, MaxTokens: maxTokens, Temperature: &temp,
		Messages: []providers.Message{{Role: "system", Content: system}, {Role: "user", Content: body}},
	}, func(ev providers.Event) error {
		if ev.Kind == providers.EventDelta {
			// Reasoning is never folded in: it would poison later summaries.
			got.WriteString(ev.Content)
			if got.Len() > maxChars*3 {
				return errSummaryOversize
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, errSummaryOversize) {
		return 0, "", fmt.Errorf("compression request failed: %w", err)
	}
	clean, err := cleanSummary(got.String(), maxChars)
	if err != nil {
		return 0, "", err
	}
	return fed, clean, nil
}

// cleanSummary strips reasoning blocks and labels, accepts mildly long output by
// cutting at a sentence end, and rejects empty or far-oversized output.
func cleanSummary(raw string, maxChars int) (string, error) {
	s := strings.TrimSpace(thinkBlock.ReplaceAllString(raw, ""))
	s = strings.TrimSpace(summaryLabel.ReplaceAllString(s, ""))
	switch {
	case s == "":
		return "", errors.New("the compression model returned no summary")
	case len(s) > maxChars*3/2:
		return "", fmt.Errorf("the compression model returned %d characters, far over the %d limit", len(s), maxChars)
	case len(s) <= maxChars:
		return s, nil
	}
	cut := s[:maxChars]
	if i := strings.LastIndexAny(cut, ".!?"); i > maxChars/2 {
		return strings.TrimSpace(cut[:i+1]), nil
	}
	return "", fmt.Errorf("the compression model returned %d characters without a place to cut at the %d limit", len(s), maxChars)
}

func clipText(s string, max int) string {
	if len(s) <= max {
		return s
	}
	s = s[:max]
	for len(s) > 0 && s[len(s)-1]&0xC0 == 0x80 {
		s = s[:len(s)-1]
	}
	return s + " […]"
}

// prepareRetrieval builds the Retrieve callback. Nothing here calls a model unless
// semantic retrieval is enabled, an embedding model is chosen, and that model is loaded.
func (rn *Runner) prepareRetrieval(ctx context.Context, in memoryInput, s memory.Settings, out *memoryRun) {
	var book *memory.Book
	if in.Character != nil && in.Character.Storybook != "" && s.RetrievalEnabled {
		b, err := memory.ParseStorybook(in.Character.Storybook)
		if err != nil {
			out.Notes = append(out.Notes, "storybook ignored: "+err.Error())
		} else {
			book = b
		}
	}
	rec := memory.Record{At: in.Now, Enabled: s.RetrievalEnabled, Candidates: []memory.CandidateRecord{}, Injected: []string{}}
	if book.Empty() {
		rec.Reason = "no storybook authored for this character"
		if !s.RetrievalEnabled {
			rec.Reason = "retrieval disabled"
		}
	} else {
		rec.Reason = "no context headroom for retrieval"
		rec.Counts = book.Counts
	}
	out.Record = &rec
	out.Keep = !book.Empty()
	if book.Empty() {
		return
	}
	// The trigger surface is the recent live window plus the message being sent.
	var window []string
	live := out.History
	for _, m := range live {
		window = append(window, m.Content)
	}
	window = append(window, in.User)
	if n := s.WindowMessages; len(window) > n {
		window = window[len(window)-n:]
	}
	out.Retrieve = func(headroom int) []memory.Injection {
		sem := rn.semanticFor(ctx, s, book, window, out)
		warmth := rn.warmthCopy(in.Conv.ID)
		inj, r := memory.Retrieve(memory.Input{Book: book, Window: window, Settings: s, Free: headroom, Warmth: warmth, Semantic: sem, Now: in.Now})
		*out.Record = r
		out.WarmthNext, out.ConvID, out.commit = warmth, in.Conv.ID, rn.commitWarmth
		return inj
	}
}

// semanticFor gathers query and document vectors, using the cache and one embedding
// request at most. It returns nil when semantic retrieval was not requested.
func (rn *Runner) semanticFor(ctx context.Context, s memory.Settings, book *memory.Book, window []string, out *memoryRun) *memory.Semantic {
	if !s.RetrieverA {
		return nil
	}
	sem := &memory.Semantic{}
	fail := func(reason string) *memory.Semantic {
		sem.Available, sem.Reason = false, reason
		return sem
	}
	if s.EmbeddingEndpointID == nil {
		return fail("no embedding model chosen")
	}
	sem.Model = s.EmbeddingModel
	info := rn.modelInfo(ctx, *s.EmbeddingEndpointID, s.EmbeddingModel)
	switch {
	case info == nil:
		return fail("embedding model is not in the endpoint inventory; refresh its models")
	case info.Type != "embeddings":
		return fail("chosen model is not an embedding model")
	case info.State == "not-loaded":
		// Never request it: LM Studio would load it just-in-time.
		return fail("embedding model is not loaded; load it in LM Studio to enable semantic retrieval")
	}
	ep, err := rn.Store.GetEndpoint(ctx, *s.EmbeddingEndpointID)
	if err != nil || !ep.Enabled {
		return fail("embedding endpoint unavailable")
	}
	docs := book.Docs()
	hashes := make([]string, len(docs))
	for i, d := range docs {
		hashes[i] = memory.Hash(s.EmbedDocPrefix + d.Text)
	}
	cached, err := rn.Store.GetEmbeddings(ctx, ep.ID, s.EmbeddingModel, hashes)
	if err != nil {
		return fail("embedding cache unreadable")
	}
	sem.Docs = map[string][]float32{}
	var missing []int
	for i, d := range docs {
		if v, ok := cached[hashes[i]]; ok {
			sem.Docs[d.ID] = v
		} else {
			missing = append(missing, i)
		}
	}
	if len(missing) > embedDocsPerTurn {
		sem.PendingDocs = len(missing) - embedDocsPerTurn
		missing = missing[:embedDocsPerTurn]
	}
	query := clipText(strings.Join(window, "\n"), maxQueryBytes)
	inputs := []string{s.EmbedQueryPrefix + query}
	for _, i := range missing {
		inputs = append(inputs, s.EmbedDocPrefix+docs[i].Text)
	}
	target, err := rn.targetFor(ctx, ep)
	if err != nil {
		return fail("embedding endpoint credential unavailable")
	}
	callCtx, cancel := context.WithTimeout(ctx, embedTimeout)
	defer cancel()
	release, err := rn.Slots.Acquire(callCtx, ep.ID, ep.ConcurrencyLimit)
	if err != nil {
		return fail("embedding endpoint busy")
	}
	res, err := openai.New(rn.Egress, target).Embed(callCtx, providers.EmbedRequest{Model: s.EmbeddingModel, Inputs: inputs})
	release()
	if err != nil {
		var pe *providers.Error
		if errors.As(err, &pe) {
			return fail("embedding request failed: " + string(pe.Kind))
		}
		return fail("embedding request failed")
	}
	if len(res.Vectors) != len(inputs) {
		return fail("embedding response had the wrong size")
	}
	sem.Query = res.Vectors[0]
	fresh := map[string][]float32{}
	for j, i := range missing {
		v := res.Vectors[j+1]
		if len(v) != len(sem.Query) {
			return fail("embedding dimensions are inconsistent")
		}
		sem.Docs[docs[i].ID] = v
		fresh[hashes[i]] = v
	}
	if len(fresh) > 0 {
		if err := persistWrite(func(c context.Context) error { return rn.Store.PutEmbeddings(c, ep.ID, s.EmbeddingModel, fresh) }); err != nil {
			out.Notes = append(out.Notes, "embedding cache write failed")
		}
	}
	sem.NewEmbeddings = len(fresh)
	sem.Available = true
	return sem
}

// finish folds what prompt assembly decided into the record: summary use, messages that
// did not fit, and notes from compression. It must run after Assemble (which is what
// invokes the retrieval callback that fills the rest of the record).
func (m *memoryRun) finish(s *prompt.Snapshot) {
	if m.Record == nil {
		return
	}
	notes := append([]string(nil), m.Notes...)
	if s.OmittedMessages > 0 {
		notes = append(notes, fmt.Sprintf("%d earlier message(s) did not fit and were left out of the prompt without a summary", s.OmittedMessages))
	}
	if s.SummaryOmitted {
		notes = append(notes, "the summary was left out because it does not fit in this context window")
	}
	m.Record.ContextNotes = notes
	m.Record.Context = memory.ContextRecord{Limit: s.ContextLimit, Estimated: s.EstimatedTokens, SummarySeq: s.SummarySeq,
		ArchivedMessages: s.ArchivedMessages, OmittedMessages: s.OmittedMessages, RetrievedCost: s.RetrievedCost}
	if len(notes) > 0 || s.SummarySeq > 0 {
		m.Keep = true
	}
}
