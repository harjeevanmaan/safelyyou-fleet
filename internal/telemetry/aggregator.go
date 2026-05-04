// Package telemetry maintains in-memory, per-device statistics.
//
// Each known device gets its own aggregator (registered once at startup);
// aggregators are independent and lock themselves, so unrelated devices
// never block each other. Heartbeats are deduped by minute (defensive
// against clock drift / duplicate deliveries); upload-time samples are
// folded into a running sum/count so memory stays O(unique-minutes) per
// device.
package telemetry

import (
	"sync"
	"time"
)

// Stats is the snapshot returned by GET /stats.
type Stats struct {
	Uptime        float64
	AvgUploadTime time.Duration
}

// aggregator holds the running statistics for a single device. All public
// methods are safe for concurrent use.
type aggregator struct {
	mu sync.Mutex

	heartbeatMinutes map[int64]struct{}
	uploadSumNanos   int64
	uploadCount      int64
}

func newAggregator() *aggregator {
	return &aggregator{heartbeatMinutes: make(map[int64]struct{})}
}

// Heartbeat records that the device was alive during the minute containing
// sentAt. Duplicate heartbeats within the same minute count as one.
func (a *aggregator) Heartbeat(sentAt time.Time) {
	minuteBucket := sentAt.UTC().Unix() / 60
	a.mu.Lock()
	defer a.mu.Unlock()
	a.heartbeatMinutes[minuteBucket] = struct{}{}
}

// Upload folds an upload-time sample into the running mean.
func (a *aggregator) Upload(uploadTime time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.uploadSumNanos += int64(uploadTime)
	a.uploadCount++
}

// Stats returns the device's current uptime and average upload time.
func (a *aggregator) Stats() Stats {
	a.mu.Lock()
	defer a.mu.Unlock()
	return Stats{
		Uptime:        a.uptimePercent(),
		AvgUploadTime: a.averageUploadTime(),
	}
}

// uptimePercent returns uptime% per the spec formula. Caller must hold a.mu.
func (a *aggregator) uptimePercent() float64 {
	if len(a.heartbeatMinutes) == 0 {
		return 0
	}

	var firstMinute, lastMinute int64
	seenAny := false
	for minute := range a.heartbeatMinutes {
		if !seenAny {
			firstMinute, lastMinute, seenAny = minute, minute, true
			continue
		}
		if minute < firstMinute {
			firstMinute = minute
		}
		if minute > lastMinute {
			lastMinute = minute
		}
	}

	windowMinutes := lastMinute - firstMinute
	if windowMinutes <= 0 {
		return 100
	}
	return float64(len(a.heartbeatMinutes)) / float64(windowMinutes) * 100
}

// averageUploadTime returns the mean upload time, or zero if no samples. Caller must hold a.mu.
func (a *aggregator) averageUploadTime() time.Duration {
	if a.uploadCount == 0 {
		return 0
	}
	return time.Duration(a.uploadSumNanos / a.uploadCount)
}
