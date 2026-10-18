package interchange

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGobboNetExportCompatibility(t *testing.T) {
	// Pinned js/21-data.js envelope with user-variant parked continuation and
	// assistant rerolls. The active top-level values are authoritative.
	data := []byte(`{"gobbonet_export":"full","version":1,"schemaVersion":1,
 "characterCards":[{"id":"c","name":"Guide","writingStyle":"Concise","startingLore":"World","greeting":"Hi {{user}}","ragStorybook":"","topP":0.9,"avatar":"https://example.com/remote.png","customCode":"evil()","customCodeEnabled":true}],
 "personaCards":[{"id":"p","name":"Visitor","description":"Explorer","injectionFrequency":0}],
 "activeCardId":"c","activePersonaId":"p","settings":{"searchKey":"secret","modelName":"unloaded"},"extensions":[{"enabled":true,"script":"evil()"}],"schedules":[{"enabled":true}],
 "folders":[{"id":"f","name":"Stories"}],"threads":[{"id":"t","name":"Branch test","cardId":"c","personaId":"p","folderId":"f","tags":["world"],"pinned":true,"messages":[
 {"role":"user","content":"new","personaId":"p","activeVariant":1,"variants":[{"content":"old","continuation":[{"role":"assistant","content":"old answer","cardId":"c"}]},{"content":"stale cached new"}]},
 {"role":"assistant","content":"new answer","cardId":"c","activeVariant":1,"variants":[{"content":"reroll one"},{"content":"cached reroll two"}]},
 {"role":"user","content":"continue"}]}]}`)
	p, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	s := p.Snapshot
	if len(s.Cards) != 2 || len(s.Conversations) != 1 || s.Defaults.Character != "c1" || s.Defaults.Persona != "c2" {
		t.Fatalf("bad mapping: %+v", s)
	}
	if s.Cards[0].Card.Style != "Concise" || s.Cards[0].Card.Portrait != "" || !strings.Contains(s.Cards[0].Card.Extra, "evil()") || s.Cards[1].Card.PersonaFrequency != 0 {
		t.Fatalf("bad card mapping: %+v", s.Cards)
	}
	c := s.Conversations[0]
	if c.Title != "Branch test" || c.Folder != "Stories" || !c.Pinned || c.Character != "c1" || c.Persona != "c2" || len(c.Messages) != 7 {
		t.Fatalf("bad conversation: %+v", c)
	}
	byKey := map[string]SnapMessage{}
	for _, m := range c.Messages {
		byKey[m.Key] = m
	}
	var path []string
	for key := c.Active; key != ""; key = byKey[key].Parent {
		path = append(path, byKey[key].Content)
	}
	if strings.Join(path, "/") != "continue/new answer/new" {
		t.Fatal(path)
	}
	if c.Messages[0].Speaker.Name != "Visitor" || c.Messages[1].Speaker.Name != "Guide" || c.Messages[1].Parent != c.Messages[0].Key {
		t.Fatal("lost historical cast/parked branch")
	}
	encoded, _ := json.Marshal(s)
	if strings.Contains(string(encoded), "secret") || strings.Contains(string(encoded), "unloaded") || strings.Contains(string(encoded), "extensions") {
		t.Fatal("activated/imported settings")
	}
	if len(p.Items) < 3 || len(p.Items[0].Notes) < 2 {
		t.Fatal("missing compatibility report")
	}
}

func TestGobboNetRejectsMalformedAndFutureExports(t *testing.T) {
	for _, data := range []string{
		`{"gobbonet_export":"full","version":2}`,
		`{"gobbonet_export":"full","version":1,"schemaVersion":2}`,
		`{"gobbonet_export":"unknown","version":1}`,
		`{"gobbonet_export":"cards","version":1,"characterCards":[{"id":"a","name":"A"},{"id":"a","name":"B"}]}`,
		`{"gobbonet_export":"threads","version":1,"threads":[{"id":"t","messages":[{"role":"system","content":"x"}]}]}`,
		`{"gobbonet_export":"threads","version":1,"threads":[{"id":"t","messages":[{"role":"user","content":"x","activeVariant":2,"variants":[{"content":"x"}]}]}]}`,
		`{"gobbonet_export":"cards","version":1,"characterCards":[{"id":"a","name":"A","temperature":99}]}`,
		`{"gobbonet_export":"threads","version":1,"threads":{}}`,
	} {
		t.Run(data, func(t *testing.T) {
			if _, err := Parse([]byte(data)); err == nil {
				t.Fatal("accepted invalid export")
			}
		})
	}
}

func TestGobboNetLinearHistoryAndNestingBounds(t *testing.T) {
	var messages []gobboMessage
	for i := 0; i < 300; i++ {
		messages = append(messages, gobboMessage{Role: "user", Content: "line"})
	}
	wrap := func(ms []gobboMessage) []byte {
		b, _ := json.Marshal(map[string]any{"gobbonet_export": "threads", "version": 1, "threads": []gobboThread{{ID: "t", Title: "Long", Messages: ms}}})
		return b
	}
	p, err := Parse(wrap(messages))
	if err != nil || len(p.Snapshot.Conversations[0].Messages) != 300 {
		t.Fatalf("linear history: %v", err)
	}
	m := gobboMessage{Role: "user", Content: "leaf"}
	for i := 0; i < 66; i++ {
		m = gobboMessage{Role: "user", Content: "active", Variants: []gobboVariant{{Content: "active"}, {Content: "other", Continuation: []gobboMessage{m}}}}
	}
	if _, err := Parse(wrap([]gobboMessage{m})); err == nil || !strings.Contains(err.Error(), "nesting") {
		t.Fatalf("unbounded nesting: %v", err)
	}
}

func TestGobboNetExpandedBranchSizeBound(t *testing.T) {
	// A compact file must not multiply one large continuation into a huge import.
	m := gobboMessage{Role: "assistant", Content: "x", Variants: make([]gobboVariant, 64)}
	for i := range m.Variants {
		m.Variants[i].Content = "x"
	}
	large := gobboMessage{Role: "user", Content: strings.Repeat("x", maxMessageBytes)}
	b, _ := json.Marshal(map[string]any{"gobbonet_export": "threads", "version": 1, "threads": []gobboThread{{ID: "t", Messages: []gobboMessage{m, large}}}})
	if _, err := Parse(b); err == nil || !strings.Contains(err.Error(), "64 MiB") {
		t.Fatalf("expanded size: %v", err)
	}
}
