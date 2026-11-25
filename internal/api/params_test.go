package api

import (
	"net/http/httptest"
	"testing"

	"github.com/sergiodavidfv/webhook-guard/internal/store"
)

func TestQueryLimit(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  int
	}{
		{name: "default", query: "", want: 25},
		{name: "valid", query: "?limit=10", want: 10},
		{name: "zero clamps to one", query: "?limit=0", want: 1},
		{name: "negative clamps to one", query: "?limit=-5", want: 1},
		{name: "above max clamps", query: "?limit=500", want: 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := queryLimit(httptest.NewRequest("GET", "/"+tt.query, nil))
			if err != nil {
				t.Fatalf("queryLimit() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("queryLimit() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestQueryLimit_NonIntegerIsRejected(t *testing.T) {
	_, err := queryLimit(httptest.NewRequest("GET", "/?limit=ten", nil))
	if err == nil {
		t.Fatal("queryLimit() error = nil, want a parameter error")
	}
}

func TestQueryOffset(t *testing.T) {
	got, err := queryOffset(httptest.NewRequest("GET", "/?offset=-3", nil))
	if err != nil {
		t.Fatalf("queryOffset() error = %v", err)
	}
	if got != 0 {
		t.Errorf("queryOffset() = %d, want 0", got)
	}
}

func TestQueryState(t *testing.T) {
	valid := []string{"", "created", "authorized", "captured", "failed", "refunded"}
	for _, raw := range valid {
		got, err := queryState(httptest.NewRequest("GET", "/?state="+raw, nil))
		if err != nil {
			t.Errorf("queryState(%q) error = %v", raw, err)
			continue
		}
		if string(got) != raw {
			t.Errorf("queryState(%q) = %q", raw, got)
		}
	}

	if _, err := queryState(httptest.NewRequest("GET", "/?state=refund", nil)); err == nil {
		t.Error("queryState(refund) error = nil, want rejection")
	}
}

func TestQueryEventStatus(t *testing.T) {
	for _, raw := range []string{"", "applied", "pending"} {
		got, err := queryEventStatus(httptest.NewRequest("GET", "/?status="+raw, nil))
		if err != nil {
			t.Errorf("queryEventStatus(%q) error = %v", raw, err)
			continue
		}
		if got != raw {
			t.Errorf("queryEventStatus(%q) = %q", raw, got)
		}
	}
	if _, err := queryEventStatus(httptest.NewRequest("GET", "/?status=done", nil)); err == nil {
		t.Error("queryEventStatus(done) error = nil, want rejection")
	}
}

func TestQueryBeforeID(t *testing.T) {
	got, err := queryBeforeID(httptest.NewRequest("GET", "/?before_id=42", nil))
	if err != nil {
		t.Fatalf("queryBeforeID() error = %v", err)
	}
	if got != 42 {
		t.Errorf("queryBeforeID() = %d, want 42", got)
	}
	if _, err := queryBeforeID(httptest.NewRequest("GET", "/?before_id=-1", nil)); err == nil {
		t.Error("queryBeforeID(-1) error = nil, want rejection")
	}
}

func TestQueryBool(t *testing.T) {
	value, present, err := queryBool(httptest.NewRequest("GET", "/?resolved=false", nil), "resolved")
	if err != nil {
		t.Fatalf("queryBool() error = %v", err)
	}
	if !present || value {
		t.Errorf("queryBool() = (%v, %v), want (false, true)", value, present)
	}

	if _, _, err := queryBool(httptest.NewRequest("GET", "/?resolved=yes", nil), "resolved"); err == nil {
		t.Error("queryBool(yes) error = nil, want rejection")
	}
}

func TestQueryMinutes(t *testing.T) {
	tests := []struct {
		query string
		want  int
	}{
		{query: "", want: 60},
		{query: "?minutes=5", want: 5},
		{query: "?minutes=0", want: 1},
		{query: "?minutes=99999", want: 1440},
	}
	for _, tt := range tests {
		got, err := queryMinutes(httptest.NewRequest("GET", "/"+tt.query, nil))
		if err != nil {
			t.Errorf("queryMinutes(%q) error = %v", tt.query, err)
			continue
		}
		if got != tt.want {
			t.Errorf("queryMinutes(%q) = %d, want %d", tt.query, got, tt.want)
		}
	}
}

func TestSameOrigin(t *testing.T) {
	tests := []struct {
		name   string
		host   string
		origin string
		site   string
		fwd    string
		want   bool
	}{
		{name: "no origin", host: "localhost:4321", want: true},
		{name: "same origin", host: "localhost:4321", origin: "http://localhost:4321", want: true},
		{name: "forwarded host", host: "app:8080", origin: "http://localhost:4321", fwd: "localhost:4321", want: true},
		{name: "different origin", host: "localhost:4321", origin: "http://evil.example", want: false},
		{name: "cross site", host: "localhost:4321", site: "cross-site", want: false},
		{name: "same site navigation", host: "localhost:4321", site: "same-site", want: false},
		{name: "malformed origin", host: "localhost:4321", origin: "://nope", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/api/v1/demo/webhooks", nil)
			r.Host = tt.host
			if tt.origin != "" {
				r.Header.Set("Origin", tt.origin)
			}
			if tt.site != "" {
				r.Header.Set("Sec-Fetch-Site", tt.site)
			}
			if tt.fwd != "" {
				r.Header.Set("X-Forwarded-Host", tt.fwd)
			}
			if got := sameOrigin(r); got != tt.want {
				t.Errorf("sameOrigin() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestChargeStateAliasesMatchStore(t *testing.T) {
	// Guard against drift between the API allowlist and the store enum.
	for _, state := range []store.ChargeState{
		store.StateCreated, store.StateAuthorized, store.StateCaptured,
		store.StateFailed, store.StateRefunded,
	} {
		if _, err := queryState(httptest.NewRequest("GET", "/?state="+string(state), nil)); err != nil {
			t.Errorf("queryState(%q) rejected a store state: %v", state, err)
		}
	}
}
