package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/Labyricorn/Labbonet/internal/secrets"
	"github.com/Labyricorn/Labbonet/internal/store/storetest"
)

func newEndpointsHandler(t *testing.T) (*Endpoints, *http.ServeMux) {
	t.Helper()
	st := storetest.New(t)
	e := &Endpoints{Store: st, Protector: secrets.NewProtector()}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/endpoints", e.List)
	mux.HandleFunc("POST /api/endpoints", e.Create)
	mux.HandleFunc("PATCH /api/endpoints/{id}", e.Patch)
	mux.HandleFunc("DELETE /api/endpoints/{id}", e.Delete)
	mux.HandleFunc("PUT /api/endpoints/{id}/credential", e.SetCredential)
	mux.HandleFunc("DELETE /api/endpoints/{id}/credential", e.ClearCredential)
	return e, mux
}

func doJSON(t *testing.T, mux *http.ServeMux, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr
}

func TestEndpoints_CreateListPatchDelete(t *testing.T) {
	_, mux := newEndpointsHandler(t)

	rr := doJSON(t, mux, http.MethodPost, "/api/endpoints", createEndpointRequest{
		Name:    "Strata",
		BaseURL: "http://127.0.0.1:8080",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var created endpointResponse
	if err := json.NewDecoder(rr.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.PrivacyClass != "local" || created.ProviderType != "openai" || created.CredentialSet {
		t.Fatalf("created endpoint = %+v", created)
	}

	rr = doJSON(t, mux, http.MethodGet, "/api/endpoints", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("list status = %d", rr.Code)
	}
	var list []endpointResponse
	if err := json.NewDecoder(rr.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("list len = %d, want 1", len(list))
	}

	id := strconv.FormatInt(created.ID, 10)
	newName := "Strata (renamed)"
	disabled := false
	rr = doJSON(t, mux, http.MethodPatch, "/api/endpoints/"+id, patchEndpointRequest{
		Name:    &newName,
		Enabled: &disabled,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("patch status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var patched endpointResponse
	if err := json.NewDecoder(rr.Body).Decode(&patched); err != nil {
		t.Fatalf("decode patch response: %v", err)
	}
	if patched.Name != newName || patched.Enabled {
		t.Fatalf("patched endpoint = %+v", patched)
	}

	rr = doJSON(t, mux, http.MethodDelete, "/api/endpoints/"+id, nil)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d", rr.Code)
	}

	rr = doJSON(t, mux, http.MethodGet, "/api/endpoints", nil)
	list = nil
	if err := json.NewDecoder(rr.Body).Decode(&list); err != nil {
		t.Fatalf("decode list after delete: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("list after delete len = %d, want 0", len(list))
	}
}

// S10: an endpoint resolving to a cloud metadata address is refused at
// registration.
func TestEndpoints_Create_RefusesMetadataAddress(t *testing.T) {
	_, mux := newEndpointsHandler(t)
	rr := doJSON(t, mux, http.MethodPost, "/api/endpoints", createEndpointRequest{
		Name:    "evil",
		BaseURL: "http://169.254.169.254/",
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400", rr.Code, rr.Body.String())
	}
}

// An external endpoint needs explicit acknowledgement before it can be
// registered (SECURITY-MODEL.md section 5).
func TestEndpoints_Create_ExternalNeedsAcknowledgement(t *testing.T) {
	_, mux := newEndpointsHandler(t)

	rr := doJSON(t, mux, http.MethodPost, "/api/endpoints", createEndpointRequest{
		Name:    "public",
		BaseURL: "http://8.8.8.8:443",
	})
	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s, want 409", rr.Code, rr.Body.String())
	}

	rr = doJSON(t, mux, http.MethodPost, "/api/endpoints", createEndpointRequest{
		Name:                "public",
		BaseURL:             "http://8.8.8.8:443",
		AcknowledgeExternal: true,
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s, want 201", rr.Code, rr.Body.String())
	}
	var created endpointResponse
	if err := json.NewDecoder(rr.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.PrivacyClass != "external" {
		t.Fatalf("privacy_class = %q, want external", created.PrivacyClass)
	}
}

// S12, S13: the credential is write-only. Setting it flips
// credential_set in list responses, but the secret value itself never
// appears in any response body.
func TestEndpoints_SetAndClearCredential(t *testing.T) {
	_, mux := newEndpointsHandler(t)

	rr := doJSON(t, mux, http.MethodPost, "/api/endpoints", createEndpointRequest{
		Name:    "Strata",
		BaseURL: "http://127.0.0.1:8080",
	})
	var created endpointResponse
	if err := json.NewDecoder(rr.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	id := strconv.FormatInt(created.ID, 10)

	const secretValue = "sk-super-secret-value"
	rr = doJSON(t, mux, http.MethodPut, "/api/endpoints/"+id+"/credential", setCredentialRequest{Value: secretValue})
	if rr.Code != http.StatusNoContent {
		t.Fatalf("set credential status = %d, body = %s", rr.Code, rr.Body.String())
	}

	rr = doJSON(t, mux, http.MethodGet, "/api/endpoints", nil)
	if strings.Contains(rr.Body.String(), secretValue) {
		t.Fatalf("list response leaked the credential value: %s", rr.Body.String())
	}
	var list []endpointResponse
	if err := json.NewDecoder(rr.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 1 || !list[0].CredentialSet {
		t.Fatalf("list = %+v, want one endpoint with credential_set=true", list)
	}

	rr = doJSON(t, mux, http.MethodDelete, "/api/endpoints/"+id+"/credential", nil)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("clear credential status = %d", rr.Code)
	}

	rr = doJSON(t, mux, http.MethodGet, "/api/endpoints", nil)
	list = nil
	if err := json.NewDecoder(rr.Body).Decode(&list); err != nil {
		t.Fatalf("decode list after clear: %v", err)
	}
	if len(list) != 1 || list[0].CredentialSet {
		t.Fatalf("list after clear = %+v, want credential_set=false", list)
	}
}

func TestEndpoints_Create_RejectsInvalidBaseURL(t *testing.T) {
	_, mux := newEndpointsHandler(t)
	rr := doJSON(t, mux, http.MethodPost, "/api/endpoints", createEndpointRequest{
		Name:    "bad",
		BaseURL: "http://127.0.0.1:8080/v1",
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400", rr.Code, rr.Body.String())
	}
}
