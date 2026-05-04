package telemetry

import "time"

// Store holds one aggregator per device. Calls for an unregistered device
// panic — the API layer is expected to validate against the device registry
// first, so reaching here with an unknown ID is a programming error.
type Store struct {
	aggregators map[string]*aggregator
}

func NewStore() *Store {
	return &Store{aggregators: make(map[string]*aggregator)}
}

// Register adds an empty aggregator for deviceID. Calling Register twice for
// the same ID is a no-op. Not safe to call concurrently with telemetry methods.
func (s *Store) Register(deviceID string) {
	if _, alreadyRegistered := s.aggregators[deviceID]; alreadyRegistered {
		return
	}
	s.aggregators[deviceID] = newAggregator()
}

// RecordHeartbeat marks that deviceID was alive during the minute of sentAt.
func (s *Store) RecordHeartbeat(deviceID string, sentAt time.Time) {
	s.mustGet(deviceID).Heartbeat(sentAt)
}

// RecordUpload records an upload-time sample for deviceID.
func (s *Store) RecordUpload(deviceID string, uploadTime time.Duration) {
	s.mustGet(deviceID).Upload(uploadTime)
}

// Snapshot returns the current statistics for deviceID.
func (s *Store) Snapshot(deviceID string) Stats {
	return s.mustGet(deviceID).Stats()
}

func (s *Store) mustGet(deviceID string) *aggregator {
	device, registered := s.aggregators[deviceID]
	if !registered {
		panic("telemetry: device " + deviceID + " not registered")
	}
	return device
}
