package endpoints

import (
	"errors"
	"testing"

	"github.com/Labyricorn/Labbonet/internal/egress"
)

func TestValidateBaseURL(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{"valid http", "http://127.0.0.1:8080", false},
		{"valid https", "https://10.138.11.11:1234", false},
		{"trailing slash ok", "http://127.0.0.1:8080/", false},
		{"empty", "", true},
		{"missing scheme", "127.0.0.1:8080", true},
		{"bad scheme", "ftp://127.0.0.1:8080", true},
		{"userinfo rejected", "http://user:pass@127.0.0.1:8080", true},
		{"path rejected", "http://127.0.0.1:8080/v1", true},
		{"query rejected", "http://127.0.0.1:8080?x=1", true},
		{"fragment rejected", "http://127.0.0.1:8080#frag", true},
		{"no host", "http:///path", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u, err := ValidateBaseURL(c.raw)
			if (err != nil) != c.wantErr {
				t.Fatalf("ValidateBaseURL(%q) err = %v, wantErr %v", c.raw, err, c.wantErr)
			}
			if err == nil && (u.Path != "" || u.RawQuery != "" || u.Fragment != "") {
				t.Fatalf("ValidateBaseURL(%q) left path/query/fragment set: %+v", c.raw, u)
			}
		})
	}
}

func TestClassifyAddress(t *testing.T) {
	ctx := t.Context()

	t.Run("loopback is local", func(t *testing.T) {
		class, err := ClassifyAddress(ctx, "127.0.0.1", false)
		if err != nil || class != egress.ClassLocal {
			t.Fatalf("class=%q err=%v, want local, nil", class, err)
		}
	})

	t.Run("RFC1918 is lan", func(t *testing.T) {
		class, err := ClassifyAddress(ctx, "10.138.11.11", false)
		if err != nil || class != egress.ClassLAN {
			t.Fatalf("class=%q err=%v, want lan, nil", class, err)
		}
	})

	t.Run("public address needs acknowledgement", func(t *testing.T) {
		if _, err := ClassifyAddress(ctx, "8.8.8.8", false); !errors.Is(err, ErrExternalNeedsAcknowledgement) {
			t.Fatalf("err = %v, want ErrExternalNeedsAcknowledgement", err)
		}
		class, err := ClassifyAddress(ctx, "8.8.8.8", true)
		if err != nil || class != egress.ClassExternal {
			t.Fatalf("class=%q err=%v, want external, nil", class, err)
		}
	})

	t.Run("metadata address refused even when acknowledged", func(t *testing.T) {
		if _, err := ClassifyAddress(ctx, "169.254.169.254", true); !errors.Is(err, ErrMetadataAddress) {
			t.Fatalf("err = %v, want ErrMetadataAddress", err)
		}
	})
}
