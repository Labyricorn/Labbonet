package openai

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/Labyricorn/Labbonet/internal/egress"
	"github.com/Labyricorn/Labbonet/internal/providers"
	"github.com/Labyricorn/Labbonet/internal/providers/fakeprovider"
)

func targetFor(t *testing.T, rawURL string) egress.Target {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse %q: %v", rawURL, err)
	}
	return egress.Target{BaseURL: u, PrivacyClass: egress.ClassLocal}
}

func TestDetect_Strata(t *testing.T) {
	srv := fakeprovider.New(fakeprovider.Script{
		HealthStatus: 200,
		HealthBody:   `{"status":"ok","service":"strata"}`,
	})
	defer srv.Close()

	a := New(egress.NewClient(), targetFor(t, srv.URL))
	d, err := a.Detect(context.Background())
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if d.DetectedService != ProfileStrata || !d.Healthy {
		t.Fatalf("Detect = %+v", d)
	}
	if a.Profile() != ProfileStrata {
		t.Fatalf("Profile() = %q", a.Profile())
	}
}

func TestDetect_LMStudio(t *testing.T) {
	srv := fakeprovider.New(fakeprovider.Script{
		ModelsPath: "/api/v0/models",
		ModelsBody: `{"data":[{"id":"m1","type":"llm","state":"not-loaded"}]}`,
	})
	defer srv.Close()

	a := New(egress.NewClient(), targetFor(t, srv.URL))
	d, err := a.Detect(context.Background())
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if d.DetectedService != ProfileLMStudio {
		t.Fatalf("Detect = %+v", d)
	}
}

func TestDetect_Generic(t *testing.T) {
	srv := fakeprovider.New(fakeprovider.Script{
		ModelsPath: "/v1/models",
		ModelsBody: `{"data":[{"id":"m1"}]}`,
	})
	defer srv.Close()

	a := New(egress.NewClient(), targetFor(t, srv.URL))
	d, err := a.Detect(context.Background())
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if d.DetectedService != ProfileGeneric {
		t.Fatalf("Detect = %+v", d)
	}
}

func TestDetect_Unreachable(t *testing.T) {
	srv := fakeprovider.New(fakeprovider.Script{}) // every path 404s
	defer srv.Close()

	a := New(egress.NewClient(), targetFor(t, srv.URL))
	_, err := a.Detect(context.Background())
	if err == nil {
		t.Fatal("Detect did not error")
	}
	var pe *providers.Error
	if !errors.As(err, &pe) || pe.Kind != providers.ErrUnreachable {
		t.Fatalf("err = %v, want ErrUnreachable", err)
	}
}
