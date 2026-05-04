// Package api wires the HTTP layer to the device registry and the telemetry
// store. The contract is in spec/openapi.json:
//
//	POST /api/v1/devices/{device_id}/heartbeat -> 204
//	POST /api/v1/devices/{device_id}/stats     -> 204
//	GET  /api/v1/devices/{device_id}/stats     -> 200 {uptime, avg_upload_time}
//
// Errors return 404 or 500 with body {"msg": "..."}.
package api

import (
	"log/slog"
	"net/http"

	"github.com/harjeevanmaan/safelyyou-fleet/internal/telemetry"
)

// Registry abstracts the device fleet so handlers stay testable without a
// real CSV file.
type Registry interface {
	Has(deviceID string) bool
}

// New returns the HTTP handler for every server endpoint, wrapped with
// request logging.
func New(registry Registry, store *telemetry.Store, logger *slog.Logger) http.Handler {
	s := &server{registry: registry, store: store, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/devices/{device_id}/heartbeat", s.handleHeartbeat)
	mux.HandleFunc("POST /api/v1/devices/{device_id}/stats", s.handlePostStats)
	mux.HandleFunc("GET /api/v1/devices/{device_id}/stats", s.handleGetStats)
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	return s.logRequests(mux)
}

// server holds the dependencies needed by handlers and middleware.
type server struct {
	registry Registry
	store    *telemetry.Store
	logger   *slog.Logger
}
