package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/harjeevan/safelyyou-fleet/internal/telemetry"
)

type fakeRegistry struct {
	known map[string]bool
}

func (f *fakeRegistry) Has(id string) bool { return f.known[id] }

func newTestHandler(t *testing.T, knownIDs ...string) http.Handler {
	t.Helper()
	known := make(map[string]bool, len(knownIDs))
	for _, id := range knownIDs {
		known[id] = true
	}
	store := telemetry.NewStore()
	for _, id := range knownIDs {
		store.Register(id)
	}
	return New(&fakeRegistry{known: known}, store, slog.Default())
}

func do(t *testing.T, handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func TestHealthz(t *testing.T) {
	t.Parallel()
	handler := newTestHandler(t)
	rr := do(t, handler, http.MethodGet, "/healthz", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if rr.Body.String() != "ok" {
		t.Fatalf("body = %q, want %q", rr.Body.String(), "ok")
	}
}

func TestHeartbeatHappyPath(t *testing.T) {
	t.Parallel()
	handler := newTestHandler(t, "dev-1")
	rr := do(t, handler, http.MethodPost, "/api/v1/devices/dev-1/heartbeat", `{"sent_at":"2025-01-01T00:00:00Z"}`)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body = %s", rr.Code, rr.Body.String())
	}
}

func TestUnknownDeviceReturnsNotFound(t *testing.T) {
	t.Parallel()
	handler := newTestHandler(t, "dev-1")

	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"heartbeat post", http.MethodPost, "/api/v1/devices/missing/heartbeat", `{"sent_at":"2025-01-01T00:00:00Z"}`},
		{"stats post", http.MethodPost, "/api/v1/devices/missing/stats", `{"sent_at":"2025-01-01T00:00:00Z","upload_time":1}`},
		{"stats get", http.MethodGet, "/api/v1/devices/missing/stats", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rr := do(t, handler, tc.method, tc.path, tc.body)
			if rr.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404; body = %s", rr.Code, rr.Body.String())
			}
			var body errorResp
			if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body.Msg == "" {
				t.Fatalf("error response missing msg field; body = %s", rr.Body.String())
			}
		})
	}
}

func TestPostStatsHappyPath(t *testing.T) {
	t.Parallel()
	handler := newTestHandler(t, "dev-1")
	rr := do(t, handler, http.MethodPost, "/api/v1/devices/dev-1/stats",
		`{"sent_at":"2025-01-01T00:00:00Z","upload_time":1500000000}`)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body = %s", rr.Code, rr.Body.String())
	}
}

func TestGetStatsReturnsExpectedShape(t *testing.T) {
	t.Parallel()
	handler := newTestHandler(t, "dev-1")

	// 3 heartbeats across 2 one-minute intervals -> 150% (the formula can
	// exceed 100% when both window endpoints are filled; the simulator avoids
	// this).
	for _, ts := range []string{
		"2025-01-01T00:00:00Z",
		"2025-01-01T00:01:00Z",
		"2025-01-01T00:02:00Z",
	} {
		rr := do(t, handler, http.MethodPost, "/api/v1/devices/dev-1/heartbeat",
			`{"sent_at":"`+ts+`"}`)
		if rr.Code != http.StatusNoContent {
			t.Fatalf("heartbeat post: status = %d", rr.Code)
		}
	}

	// 100ms, 200ms, 300ms -> 200ms average.
	for _, ut := range []int64{100_000_000, 200_000_000, 300_000_000} {
		body, _ := json.Marshal(map[string]any{"sent_at": "2025-01-01T00:00:00Z", "upload_time": ut})
		rr := do(t, handler, http.MethodPost, "/api/v1/devices/dev-1/stats", string(body))
		if rr.Code != http.StatusNoContent {
			t.Fatalf("stats post: status = %d", rr.Code)
		}
	}

	rr := do(t, handler, http.MethodGet, "/api/v1/devices/dev-1/stats", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rr.Code, rr.Body.String())
	}

	var resp getStatsResp
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Uptime != 150 {
		t.Fatalf("uptime = %v, want 150", resp.Uptime)
	}
	if resp.AvgUploadTime != "200ms" {
		t.Fatalf("avg_upload_time = %q, want %q", resp.AvgUploadTime, "200ms")
	}
}

func TestMalformedBodyReturns500(t *testing.T) {
	t.Parallel()
	handler := newTestHandler(t, "dev-1")

	cases := []struct {
		name string
		body string
	}{
		{"empty body", ""},
		{"not json", "this is not json"},
		{"missing sent_at", `{}`},
		{"unknown field", `{"sent_at":"2025-01-01T00:00:00Z","mystery":42}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rr := do(t, handler, http.MethodPost, "/api/v1/devices/dev-1/heartbeat", tc.body)
			if rr.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500", rr.Code)
			}
		})
	}
}

func TestRoutingMethodMismatch(t *testing.T) {
	t.Parallel()
	handler := newTestHandler(t, "dev-1")
	req := httptest.NewRequest(http.MethodPut, "/api/v1/devices/dev-1/heartbeat", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rr.Code)
	}
}
