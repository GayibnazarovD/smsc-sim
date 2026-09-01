// Command smsc-sim runs a multi-operator SMPP SMSC simulator for load and
// integration testing of SMPP clients and SMS gateways.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/dilshodgayibnazarov/smsc-sim/internal/admin"
	"github.com/dilshodgayibnazarov/smsc-sim/internal/config"
	"github.com/dilshodgayibnazarov/smsc-sim/internal/metrics"
	"github.com/dilshodgayibnazarov/smsc-sim/internal/smsc"
)

// version is overwritten at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	var (
		cfgPath     = flag.String("config", "smsc-sim.yaml", "path to the YAML config file")
		showVersion = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println("smsc-sim", version)
		return
	}

	if err := run(*cfgPath); err != nil {
		fmt.Fprintln(os.Stderr, "smsc-sim:", err)
		os.Exit(1)
	}
}

func run(cfgPath string) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	log := newLogger(cfg.Log)
	log.Info("starting smsc-sim", "version", version, "operators", len(cfg.Operators), "seed", cfg.Seed)

	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	m := metrics.New(reg)

	srv := smsc.New(cfg, log, m)
	if err := srv.Start(); err != nil {
		return err
	}

	var httpServers []*http.Server
	if addr := cfg.Metrics.Addr; addr != "" {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
		httpServers = append(httpServers, serveHTTP(log, "metrics", addr, mux))
	}
	if addr := cfg.Admin.Addr; addr != "" {
		httpServers = append(httpServers, serveHTTP(log, "admin", addr, admin.Handler(srv, log)))
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	log.Info("shutdown signal received, draining")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, hs := range httpServers {
		_ = hs.Shutdown(shutdownCtx)
	}
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Warn("simulator did not drain cleanly", "err", err)
		return nil
	}
	log.Info("stopped")
	return nil
}

func serveHTTP(log *slog.Logger, name, addr string, h http.Handler) *http.Server {
	hs := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		log.Info("http listener up", "name", name, "addr", addr)
		if err := hs.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("http listener failed", "name", name, "addr", addr, "err", err)
		}
	}()
	return hs
}

func newLogger(c config.Log) *slog.Logger {
	level := slog.LevelInfo
	switch strings.ToLower(c.Level) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	opts := &slog.HandlerOptions{Level: level}
	if strings.ToLower(c.Format) == "json" {
		return slog.New(slog.NewJSONHandler(os.Stderr, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stderr, opts))
}
