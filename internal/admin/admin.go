// Package admin exposes an HTTP API and an embedded Web Admin UI for introspecting
// the simulator, managing client sessions, monitoring throughput, and injecting
// mobile-originated messages at runtime.
package admin

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/dilshodgayibnazarov/smsc-sim/internal/config"
	"github.com/dilshodgayibnazarov/smsc-sim/internal/smsc"
)

//go:embed ui/*
var uiFS embed.FS

// OverviewSnapshot provides high-level telemetry and status across all operators.
type OverviewSnapshot struct {
	Uptime               string           `json:"uptime"`
	UptimeSeconds        int64            `json:"uptime_seconds"`
	StartTime            time.Time        `json:"start_time"`
	OperatorsCount       int              `json:"operators_count"`
	ActiveBindsCount     int              `json:"active_binds_count"`
	TotalMessagesSeen    uint64           `json:"total_messages_seen"`
	DLREnabledOperators  int              `json:"dlr_enabled_operators"`
	ThrottledOperators   int              `json:"throttled_operators"`
	Operators            []smsc.Snapshot  `json:"operators"`
}

// Handler returns the admin API and Web UI mux.
//
//	GET  /                               -> Web Admin UI dashboard
//	GET  /ui/*                           -> Static UI assets (CSS, JS)
//	GET  /healthz                        -> 200 "ok"
//	GET  /admin/overview                 -> Aggregate system overview
//	GET  /admin/operators                -> [ { operator snapshot } ]
//	GET  /admin/sessions                 -> [ { session snapshot } ]
//	POST /admin/sessions/{id}/disconnect -> Disconnect an active ESME session
//	GET  /admin/events                   -> [ { event } ]
//	GET  /admin/config                   -> Active YAML config
//	POST /admin/operators/{name}/mo     -> Inject a mobile-originated message
func Handler(srv *smsc.Server, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()

	uiSub, err := fs.Sub(uiFS, "ui")
	if err != nil {
		log.Error("failed to create ui sub-filesystem", "err", err)
	} else {
		fileServer := http.FileServer(http.FS(uiSub))
		mux.Handle("GET /ui/", http.StripPrefix("/ui/", fileServer))
	}

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		data, err := uiFS.ReadFile("ui/index.html")
		if err != nil {
			http.Error(w, "UI not found: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	})

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("GET /admin/overview", func(w http.ResponseWriter, _ *http.Request) {
		ops := srv.Operators()
		snapshots := make([]smsc.Snapshot, 0, len(ops))
		var totalBinds int
		var totalMsgs uint64
		var dlrCount int
		var throttledCount int

		for _, op := range ops {
			snap := op.Describe()
			snapshots = append(snapshots, snap)
			totalBinds += snap.ActiveBinds
			totalMsgs += snap.MessagesSeen
			if snap.DLREnabled {
				dlrCount++
			}
			if snap.RateLimited {
				throttledCount++
			}
		}

		uptime := srv.Uptime()
		overview := OverviewSnapshot{
			Uptime:              formatDuration(uptime),
			UptimeSeconds:       int64(uptime.Seconds()),
			StartTime:           srv.StartTime(),
			OperatorsCount:      len(ops),
			ActiveBindsCount:    totalBinds,
			TotalMessagesSeen:   totalMsgs,
			DLREnabledOperators: dlrCount,
			ThrottledOperators:  throttledCount,
			Operators:           snapshots,
		}
		writeJSON(w, http.StatusOK, overview)
	})

	mux.HandleFunc("GET /admin/operators", func(w http.ResponseWriter, _ *http.Request) {
		out := make([]smsc.Snapshot, 0, len(srv.Operators()))
		for _, op := range srv.Operators() {
			out = append(out, op.Describe())
		}
		writeJSON(w, http.StatusOK, out)
	})

	mux.HandleFunc("GET /admin/sessions", func(w http.ResponseWriter, _ *http.Request) {
		sessions := srv.Sessions()
		writeJSON(w, http.StatusOK, sessions)
	})

	mux.HandleFunc("POST /admin/sessions/{id}/disconnect", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			writeJSON(w, http.StatusBadRequest, errBody{"session id required"})
			return
		}
		if err := srv.DisconnectSession(id); err != nil {
			writeJSON(w, http.StatusNotFound, errBody{err.Error()})
			return
		}
		log.Info("admin: disconnected session", "id", id)
		writeJSON(w, http.StatusOK, map[string]string{"status": "disconnected", "id": id})
	})

	mux.HandleFunc("GET /admin/events", func(w http.ResponseWriter, r *http.Request) {
		limit := 100
		if q := r.URL.Query().Get("limit"); q != "" {
			if n, err := strconv.Atoi(q); err == nil && n > 0 {
				limit = n
			}
		}
		events := srv.Events(limit)
		writeJSON(w, http.StatusOK, events)
	})

	mux.HandleFunc("GET /admin/config", func(w http.ResponseWriter, _ *http.Request) {
		cfg := srv.Config()
		if cfg == nil {
			cfg = &config.Config{}
		}
		writeJSON(w, http.StatusOK, cfg)
	})

	mux.HandleFunc("POST /admin/operators/{name}/mo", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Source     string `json:"source"`
			Dest       string `json:"dest"`
			Text       string `json:"text"`
			DataCoding uint8  `json:"data_coding"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errBody{"invalid JSON body: " + err.Error()})
			return
		}
		if req.Dest == "" || req.Text == "" {
			writeJSON(w, http.StatusBadRequest, errBody{"dest and text are required"})
			return
		}
		if err := srv.InjectMO(r.PathValue("name"), req.Source, req.Dest, req.Text, req.DataCoding); err != nil {
			writeJSON(w, http.StatusConflict, errBody{err.Error()})
			return
		}
		log.Info("admin: injected MO", "operator", r.PathValue("name"), "dest", req.Dest)
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "sent", "operator": r.PathValue("name"), "dest": req.Dest})
	})

	return mux
}

type errBody struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second
	if h > 0 {
		return fmt.Sprintf("%dh %02dm %02ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %02ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}
