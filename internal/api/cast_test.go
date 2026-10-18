package api

import (
	"encoding/json"
	"github.com/Labyricorn/Labbonet/internal/cast"
	"github.com/Labyricorn/Labbonet/internal/store/storetest"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCastAPICRUDStrictDecodeAndRevisionConflicts(t *testing.T) {
	st := storetest.New(t)
	c := &Cast{Store: st}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/cast", c.Save)
	mux.HandleFunc("PUT /api/cast/{id}", c.Save)
	mux.HandleFunc("DELETE /api/cast/{id}", c.Delete)
	mux.HandleFunc("GET /api/cast", c.List)
	mux.HandleFunc("PUT /api/cast/defaults", c.Selection)
	mux.HandleFunc("POST /api/conversations/{id}/greeting", c.Greet)
	rr := doJSON(t, mux, "POST", "/api/cast", cast.Card{Kind: "character", Name: "<script>inert</script>", Greetings: []string{"Hi"}})
	if rr.Code != 201 {
		t.Fatal(rr.Code, rr.Body.String())
	}
	var card cast.Card
	if err := json.Unmarshal(rr.Body.Bytes(), &card); err != nil {
		t.Fatal(err)
	}
	// The first created card's ID is one in this isolated database.
	rr = doJSON(t, mux, "PUT", "/api/cast/1", card)
	if rr.Code != 200 {
		t.Fatal(rr.Code, rr.Body.String())
	}
	rr = doJSON(t, mux, "PUT", "/api/cast/1", card)
	if rr.Code != 409 {
		t.Fatal(rr.Code)
	}
	rr = doJSON(t, mux, "DELETE", "/api/cast/1", map[string]int64{"revision": card.Revision})
	if rr.Code != 409 {
		t.Fatal(rr.Code)
	}
	for _, raw := range []string{`{"kind":"character","name":"X","unknown":1}`, `{"kind":"character","name":"X"} {}`, `{"kind":"character","name":"X","portrait":"https://example.com/x.png"}`} {
		rr = httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest("POST", "/api/cast", strings.NewReader(raw)))
		if rr.Code != 400 {
			t.Fatal(rr.Code, rr.Body.String())
		}
	}
	defaults, _ := st.GetCast(t.Context(), 0)
	defaults.CharacterID = &card.ID
	if _, err := st.SaveCast(t.Context(), 0, defaults); err != nil {
		t.Fatal(err)
	}
	conv, err := st.CreateConversation(t.Context(), "test", nil, nil, "", "{}")
	if err != nil || conv.ID != 1 {
		t.Fatal(conv, err)
	}
	rr = doJSON(t, mux, "POST", "/api/conversations/1/greeting", map[string]int{"revision": 99})
	if rr.Code != 409 {
		t.Fatal(rr.Code, rr.Body.String())
	}
	rr = doJSON(t, mux, "POST", "/api/conversations/1/greeting", map[string]int{"revision": 1})
	if rr.Code != 204 {
		t.Fatal(rr.Code, rr.Body.String())
	}
	rr = doJSON(t, mux, "POST", "/api/conversations/1/greeting", map[string]int{"revision": 1})
	if rr.Code != 409 {
		t.Fatal(rr.Code)
	}
	sel, _ := st.GetCast(t.Context(), 0)
	sel.CharacterID = new(int64)
	*sel.CharacterID = 999
	rr = doJSON(t, mux, "PUT", "/api/cast/defaults", sel)
	if rr.Code != 400 {
		t.Fatal(rr.Code)
	}
	rr = doJSON(t, mux, "GET", "/api/cast", nil)
	if rr.Code != 200 {
		t.Fatal(rr.Code)
	}
	var cards []cast.Card
	if err = json.Unmarshal(rr.Body.Bytes(), &cards); err != nil || len(cards) != 1 {
		t.Fatal(cards, err)
	}
}
