package telemetry

import (
	"math"
	"sync"
	"testing"
	"time"
)

const epsilon = 1e-9

func mustParse(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return ts
}

func TestSnapshotEmpty(t *testing.T) {
	t.Parallel()
	s := NewStore()
	s.Register("dev-1")
	got := s.Snapshot("dev-1")
	if got.Uptime != 0 || got.AvgUploadTime != 0 {
		t.Fatalf("zero-value snapshot = %+v, want all zeroes", got)
	}
}

func TestUnregisteredDevicePanics(t *testing.T) {
	t.Parallel()
	s := NewStore()
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic, got nil")
		}
	}()
	s.Snapshot("missing")
}

func TestUptimeCalculation(t *testing.T) {
	t.Parallel()

	// uptime = (uniqueMinutes / (lastMin - firstMin)) * 100. Note this can
	// exceed 100% when both window endpoints are filled — that's an artifact
	// of the spec's formula; the simulator's data avoids it.

	tests := []struct {
		name       string
		minutes    []string // RFC3339 timestamps for heartbeats
		wantUptime float64
	}{
		{
			name:       "single heartbeat is 100%",
			minutes:    []string{"2025-01-01T00:00:00Z"},
			wantUptime: 100,
		},
		{
			name: "9 heartbeats over a 10-interval window is 90%",
			minutes: []string{
				"2025-01-01T00:00:00Z",
				"2025-01-01T00:01:00Z",
				"2025-01-01T00:02:00Z",
				"2025-01-01T00:03:00Z",
				// missing 04 and 05
				"2025-01-01T00:06:00Z",
				"2025-01-01T00:07:00Z",
				"2025-01-01T00:08:00Z",
				"2025-01-01T00:09:00Z",
				"2025-01-01T00:10:00Z",
			},
			wantUptime: 90,
		},
		{
			name: "filling both window endpoints yields 110%",
			minutes: []string{
				"2025-01-01T00:00:00Z",
				"2025-01-01T00:01:00Z",
				"2025-01-01T00:02:00Z",
				"2025-01-01T00:03:00Z",
				"2025-01-01T00:04:00Z",
				"2025-01-01T00:05:00Z",
				"2025-01-01T00:06:00Z",
				"2025-01-01T00:07:00Z",
				"2025-01-01T00:08:00Z",
				"2025-01-01T00:09:00Z",
				"2025-01-01T00:10:00Z",
			},
			wantUptime: 110,
		},
		{
			name: "duplicate heartbeats in one minute count as one",
			minutes: []string{
				"2025-01-01T00:00:00Z",
				"2025-01-01T00:00:30Z",
				"2025-01-01T00:00:45Z",
				"2025-01-01T00:01:00Z",
			},
			wantUptime: 200,
		},
		{
			name: "out-of-order heartbeats compute the same window",
			minutes: []string{
				"2025-01-01T00:09:00Z",
				"2025-01-01T00:00:00Z",
				"2025-01-01T00:05:00Z",
			},
			wantUptime: float64(3) / float64(9) * 100,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := NewStore()
			s.Register("dev")
			for _, m := range tc.minutes {
				s.RecordHeartbeat("dev", mustParse(t, m))
			}
			got := s.Snapshot("dev").Uptime
			if math.Abs(got-tc.wantUptime) > epsilon {
				t.Fatalf("uptime = %v, want %v", got, tc.wantUptime)
			}
		})
	}
}

func TestAvgUploadTime(t *testing.T) {
	t.Parallel()
	s := NewStore()
	s.Register("dev")

	// Mean of 100ms, 200ms, 300ms is 200ms.
	s.RecordUpload("dev", 100*time.Millisecond)
	s.RecordUpload("dev", 200*time.Millisecond)
	s.RecordUpload("dev", 300*time.Millisecond)

	got := s.Snapshot("dev")
	if got.AvgUploadTime != 200*time.Millisecond {
		t.Fatalf("avg = %v, want 200ms", got.AvgUploadTime)
	}
}

func TestPerDeviceIsolation(t *testing.T) {
	t.Parallel()
	s := NewStore()
	s.Register("alpha")
	s.Register("beta")

	s.RecordUpload("alpha", 100*time.Millisecond)
	s.RecordUpload("beta", 500*time.Millisecond)
	s.RecordHeartbeat("alpha", mustParse(t, "2025-01-01T00:00:00Z"))

	if got := s.Snapshot("alpha").AvgUploadTime; got != 100*time.Millisecond {
		t.Fatalf("alpha avg = %v, want 100ms", got)
	}
	if got := s.Snapshot("beta").AvgUploadTime; got != 500*time.Millisecond {
		t.Fatalf("beta avg = %v, want 500ms", got)
	}
	if got := s.Snapshot("beta").Uptime; got != 0 {
		t.Fatalf("beta uptime = %v, want 0 (no heartbeats)", got)
	}
}

// TestConcurrentWritesAreSafe stress-tests the per-device locks. Run with
// `go test -race` to catch any latent data races.
func TestConcurrentWritesAreSafe(t *testing.T) {
	t.Parallel()
	s := NewStore()
	s.Register("dev")
	const goroutines = 64
	const itersPerG = 500
	base := mustParse(t, "2025-01-01T00:00:00Z")

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < itersPerG; j++ {
				ts := base.Add(time.Duration(j) * time.Minute)
				s.RecordHeartbeat("dev", ts)
				s.RecordUpload("dev", time.Duration(j+1)*time.Millisecond)
				_ = s.Snapshot("dev")
			}
		}()
	}
	wg.Wait()

	// Every minute from 0..itersPerG-1 should be covered exactly once even
	// across goroutines: itersPerG unique minutes over (itersPerG - 1)
	// intervals.
	got := s.Snapshot("dev")
	want := float64(itersPerG) / float64(itersPerG-1) * 100
	if math.Abs(got.Uptime-want) > epsilon {
		t.Fatalf("uptime = %v, want %v", got.Uptime, want)
	}
}
