package store

import "testing"

// A-E7: set, replace and clear a credential; the store never exposes the
// blob through anything but the dedicated accessor.
func TestCredentials_SetReplaceClear(t *testing.T) {
	ctx := t.Context()
	st, _ := newTestStore(t)

	ep, err := st.CreateEndpoint(ctx, "Strata", "openai", "http://127.0.0.1:8080", "local", 1)
	if err != nil {
		t.Fatalf("create endpoint: %v", err)
	}

	has, err := st.HasCredential(ctx, ep.ID)
	if err != nil {
		t.Fatalf("has (before set): %v", err)
	}
	if has {
		t.Fatalf("new endpoint already has a credential")
	}

	if err := st.SetCredential(ctx, ep.ID, []byte("blob-v1")); err != nil {
		t.Fatalf("set: %v", err)
	}
	has, err = st.HasCredential(ctx, ep.ID)
	if err != nil || !has {
		t.Fatalf("has (after set) = %v, %v", has, err)
	}
	blob, err := st.CredentialBlob(ctx, ep.ID)
	if err != nil || string(blob) != "blob-v1" {
		t.Fatalf("blob (after set) = %q, %v", blob, err)
	}

	if err := st.SetCredential(ctx, ep.ID, []byte("blob-v2")); err != nil {
		t.Fatalf("replace: %v", err)
	}
	blob, err = st.CredentialBlob(ctx, ep.ID)
	if err != nil || string(blob) != "blob-v2" {
		t.Fatalf("blob (after replace) = %q, %v", blob, err)
	}

	if err := st.ClearCredential(ctx, ep.ID); err != nil {
		t.Fatalf("clear: %v", err)
	}
	has, err = st.HasCredential(ctx, ep.ID)
	if err != nil || has {
		t.Fatalf("has (after clear) = %v, %v", has, err)
	}
}

func TestCredentials_DeletedWithEndpoint(t *testing.T) {
	ctx := t.Context()
	st, _ := newTestStore(t)

	ep, err := st.CreateEndpoint(ctx, "Strata", "openai", "http://127.0.0.1:8080", "local", 1)
	if err != nil {
		t.Fatalf("create endpoint: %v", err)
	}
	if err := st.SetCredential(ctx, ep.ID, []byte("blob")); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := st.DeleteEndpoint(ctx, ep.ID); err != nil {
		t.Fatalf("delete endpoint: %v", err)
	}
	has, err := st.HasCredential(ctx, ep.ID)
	if err != nil {
		t.Fatalf("has after endpoint delete: %v", err)
	}
	if has {
		t.Fatalf("credential survived its endpoint's deletion (foreign_keys=ON cascade not applied)")
	}
}
