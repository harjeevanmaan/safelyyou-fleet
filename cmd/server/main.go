// Command server runs the SafelyYou fleet metrics API.
//
//	server -port 6733 -csv data/devices.csv
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/harjeevanmaan/safelyyou-fleet/internal/api"
	"github.com/harjeevanmaan/safelyyou-fleet/internal/devices"
	"github.com/harjeevanmaan/safelyyou-fleet/internal/telemetry"
)

const shutdownTimeout = 10 * time.Second

type config struct {
	port int
	csv  string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// run wires everything together and blocks until the server stops.
// It returns nil on a clean Ctrl+C shutdown, or an error otherwise.
func run() error {
	cfg := parseFlags()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	registry, err := devices.LoadCSV(cfg.csv)
	if err != nil {
		return fmt.Errorf("load %s: %w", cfg.csv, err)
	}
	logger.Info("registry loaded", "count", registry.Len(), "path", cfg.csv)

	store := telemetry.NewStore()
	for _, id := range registry.IDs() {
		store.Register(id)
	}

	srv := newHTTPServer(cfg, registry, store, logger)

	// On Ctrl+C, ask the server to drain in-flight requests and stop.
	// ListenAndServe below will then return http.ErrServerClosed.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go shutdownOnSignal(ctx, srv, logger)

	logger.Info("listening", "addr", srv.Addr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("server failed: %w", err)
	}
	return nil
}

func parseFlags() config {
	var cfg config
	flag.IntVar(&cfg.port, "port", 6733, "TCP port to listen on")
	flag.StringVar(&cfg.csv, "csv", "data/devices.csv", "path to devices.csv")
	flag.Parse()
	return cfg
}

func newHTTPServer(cfg config, reg *devices.Registry, store *telemetry.Store, logger *slog.Logger) *http.Server {
	return &http.Server{
		Addr:              net.JoinHostPort("127.0.0.1", strconv.Itoa(cfg.port)),
		Handler:           api.New(reg, store, logger),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

// shutdownOnSignal blocks until ctx is canceled (a signal arrived),
// then asks the server to stop within shutdownTimeout.
func shutdownOnSignal(ctx context.Context, srv *http.Server, logger *slog.Logger) {
	<-ctx.Done()
	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "err", err)
	}
}
