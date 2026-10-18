package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/Labyricorn/Labbonet/internal/egress"
	"github.com/Labyricorn/Labbonet/internal/providers/fakeprovider"
	"github.com/Labyricorn/Labbonet/internal/secrets"
	"github.com/Labyricorn/Labbonet/internal/store/storetest"
)

func newModelsHandler(t *testing.T) (*Endpoints, *http.ServeMux) {
	t.Helper()
	st := storetest.New(t)
	eps := &Endpoints{Store: st, Protector: secrets.NewProtector()}
	models := &Models{Store: st, Protector: secrets.NewProtector(), Egress: egress.NewClient()}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/endpoints", eps.Create)
	mux.HandleFunc("GET /api/endpoints", eps.List)
	mux.HandleFunc("PUT /api/endpoints/{id}/credential", eps.SetCredential)
	mux.HandleFunc("POST /api/endpoints/{id}/test", models.Test)
	mux.HandleFunc("POST /api/endpoints/{id}/models/refresh", models.Refresh)
	mux.HandleFunc("GET /api/endpoints/{id}/models", models.List)
	return eps, mux
}

func registerEndpoint(t *testing.T, mux *http.ServeMux, baseURL string) endpointResponse {
	t.Helper()
	rr := doJSON(t, mux, http.MethodPost, "/api/endpoints", createEndpointRequest{
		Name:    "test",
		BaseURL: baseURL,
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("register endpoint: status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var ep endpointResponse
	if err := json.NewDecoder(rr.Body).Decode(&ep); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return ep
}

func TestModels_TestDetectsStrata(t *testing.T) {
	srv := fakeprovider.New(fakeprovider.Script{
		HealthStatus: 200,
		HealthBody:   `{"status":"ok","service":"strata"}`,
	})
	defer srv.Close()

	_, mux := newModelsHandler(t)
	ep := registerEndpoint(t, mux, srv.URL)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/endpoints/"+strconv.FormatInt(ep.ID, 10)+"/test", nil)
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("test status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var resp testResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.DetectedService != "strata" || !resp.Healthy {
		t.Fatalf("resp = %+v", resp)
	}

	// The detection is persisted on the endpoint.
	rr = doJSON(t, mux, http.MethodGet, "/api/endpoints", nil)
	var list []endpointResponse
	if err := json.NewDecoder(rr.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 1 || list[0].DetectedService != "strata" {
		t.Fatalf("list = %+v", list)
	}
}

func TestModels_RefreshAndCachedList(t *testing.T) {
	srv := fakeprovider.New(fakeprovider.Script{
		HealthStatus: 200,
		HealthBody:   `{"status":"ok","service":"strata"}`,
		ModelsPath:   "/v1/models",
		ModelsBody:   `{"data":[{"id":"qwen3.8-flash-next","status":{"value":"loaded"},"meta":{"n_ctx":32768}}]}`,
	})
	defer srv.Close()

	_, mux := newModelsHandler(t)
	ep := registerEndpoint(t, mux, srv.URL)
	id := strconv.FormatInt(ep.ID, 10)

	// Before any refresh, the cached list is empty.
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/endpoints/"+id+"/models", nil)
	mux.ServeHTTP(rr, req)
	var cached []modelResponse
	if err := json.NewDecoder(rr.Body).Decode(&cached); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(cached) != 0 {
		t.Fatalf("cached before refresh = %+v, want none", cached)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/endpoints/"+id+"/models/refresh", nil)
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var refreshed []modelResponse
	if err := json.NewDecoder(rr.Body).Decode(&refreshed); err != nil {
		t.Fatalf("decode refresh: %v", err)
	}
	if len(refreshed) != 1 || refreshed[0].ID != "qwen3.8-flash-next" || refreshed[0].ContextLength != 32768 {
		t.Fatalf("refreshed = %+v", refreshed)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/endpoints/"+id+"/models", nil)
	mux.ServeHTTP(rr, req)
	cached = nil
	if err := json.NewDecoder(rr.Body).Decode(&cached); err != nil {
		t.Fatalf("decode cached list: %v", err)
	}
	if len(cached) != 1 || cached[0].ID != "qwen3.8-flash-next" || cached[0].State != "loaded" {
		t.Fatalf("cached after refresh = %+v", cached)
	}
}

// S11 at the API layer: a credential set on one endpoint must never
// reach another endpoint's request, even through the /test handler.
func TestModels_Test_CredentialScopedToItsOwnEndpoint(t *testing.T) {
	srvA := fakeprovider.New(fakeprovider.Script{HealthStatus: 200, HealthBody: `{"service":"strata"}`})
	defer srvA.Close()
	srvB := fakeprovider.New(fakeprovider.Script{HealthStatus: 200, HealthBody: `{"service":"strata"}`})
	defer srvB.Close()

	_, mux := newModelsHandler(t)
	epA := registerEndpoint(t, mux, srvA.URL)
	epB := registerEndpoint(t, mux, srvB.URL)

	rr := doJSON(t, mux, http.MethodPut, "/api/endpoints/"+strconv.FormatInt(epA.ID, 10)+"/credential", setCredentialRequest{Value: "secret-a"})
	if rr.Code != http.StatusNoContent {
		t.Fatalf("set credential: status = %d", rr.Code)
	}

	for _, id := range []int64{epA.ID, epB.ID} {
		rr = httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/endpoints/"+strconv.FormatInt(id, 10)+"/test", nil)
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("test endpoint %d: status = %d, body = %s", id, rr.Code, rr.Body.String())
		}
	}

	if got := srvA.LastAuthHeader(); got != "Bearer secret-a" {
		t.Fatalf("endpoint A Authorization = %q, want Bearer secret-a", got)
	}
	if got := srvB.LastAuthHeader(); got != "" {
		t.Fatalf("endpoint B Authorization = %q, want empty", got)
	}
}
