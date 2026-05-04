package api

import (
	"errors"
	"net/http"
	"time"
)

// sent_at is *time.Time so we can distinguish "field missing" (nil) from
// "field present with the Go zero time" (non-nil). The simulator emits a
// literal "0001-01-01T00:00:00Z" on every /stats POST, which we accept.

type heartbeatReq struct {
	SentAt *time.Time `json:"sent_at"`
}

type uploadStatsReq struct {
	SentAt     *time.Time `json:"sent_at"`
	UploadTime int64      `json:"upload_time"` // nanoseconds
}

type getStatsResp struct {
	AvgUploadTime string  `json:"avg_upload_time"`
	Uptime        float64 `json:"uptime"`
}

type errorResp struct {
	Msg string `json:"msg"`
}

func (s *server) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := s.requireKnownDevice(w, r)
	if !ok {
		return
	}

	var request heartbeatReq
	if err := decodeJSON(w, r, &request); err != nil {
		s.serverError(w, err)
		return
	}
	if request.SentAt == nil {
		s.serverError(w, errors.New("sent_at is required"))
		return
	}

	s.store.RecordHeartbeat(deviceID, *request.SentAt)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handlePostStats(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := s.requireKnownDevice(w, r)
	if !ok {
		return
	}

	var request uploadStatsReq
	if err := decodeJSON(w, r, &request); err != nil {
		s.serverError(w, err)
		return
	}
	if request.SentAt == nil {
		s.serverError(w, errors.New("sent_at is required"))
		return
	}

	s.store.RecordUpload(deviceID, time.Duration(request.UploadTime))
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleGetStats(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := s.requireKnownDevice(w, r)
	if !ok {
		return
	}

	snapshot := s.store.Snapshot(deviceID)
	writeJSON(w, http.StatusOK, getStatsResp{
		AvgUploadTime: snapshot.AvgUploadTime.String(),
		Uptime:        snapshot.Uptime,
	})
}

func (s *server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// requireKnownDevice extracts the device_id path parameter and verifies it's
// in the fleet. On miss it writes a 404 and returns ok=false.
func (s *server) requireKnownDevice(w http.ResponseWriter, r *http.Request) (string, bool) {
	deviceID := r.PathValue("device_id")
	if !s.registry.Has(deviceID) {
		s.notFound(w, deviceID)
		return "", false
	}
	return deviceID, true
}

func (s *server) notFound(w http.ResponseWriter, deviceID string) {
	writeJSON(w, http.StatusNotFound, errorResp{Msg: "device " + deviceID + " not found"})
}

// serverError responds 500 with the error message. The OpenAPI spec only
// documents 404 and 500 for failures, so malformed-body errors land here too.
func (s *server) serverError(w http.ResponseWriter, err error) {
	s.logger.Warn("request rejected", "err", err)
	writeJSON(w, http.StatusInternalServerError, errorResp{Msg: err.Error()})
}
