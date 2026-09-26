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
	"github.com/dilshodgayibnazarov/smsc-sim/internal/store"
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
//	GET    /                               -> Web Admin UI dashboard
//	GET    /ui/*                           -> Static UI assets (CSS, JS)
//	GET    /healthz                        -> 200 "ok"
//	GET    /admin/overview                 -> Aggregate system overview
//	GET    /admin/operators                -> [ { operator snapshot } ]
//	POST   /admin/operators                -> Create new operator (DB & runtime)
//	GET    /admin/operators/{name}         -> Get single operator config
//	PUT    /admin/operators/{name}         -> Update operator (DB & runtime)
//	DELETE /admin/operators/{name}         -> Delete operator (DB & runtime)
//	GET    /admin/sessions                 -> [ { session snapshot } ]
//	POST   /admin/sessions/{id}/disconnect -> Disconnect an active ESME session
//	GET    /admin/events                   -> [ { event } ]
//	GET    /admin/config                   -> Active YAML config
//	POST   /admin/operators/{name}/mo     -> Inject a mobile-originated message
func Handler(srv *smsc.Server, st *store.Store, log *slog.Logger) http.Handler {
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
		if st != nil {
			stored, err := st.ListOperators()
			if err == nil && len(stored) > 0 {
				out := make([]smsc.Snapshot, 0, len(stored))
				for _, sop := range stored {
					accts := make([]string, len(sop.Accounts))
					for i, a := range sop.Accounts {
						accts[i] = a.SystemID
					}
					bt := sop.BindTypes
					if len(bt) == 0 {
						bt = []string{"tx", "rx", "trx"}
					}
					tps, burst, limited := sop.Throttle.Rate()
					var activeBinds int
					var msgsSeen uint64
					if liveOp := srv.OperatorByName(sop.Name); liveOp != nil {
						snap := liveOp.Describe()
						activeBinds = snap.ActiveBinds
						msgsSeen = snap.MessagesSeen
					}
					out = append(out, smsc.Snapshot{
						Name:          sop.Name,
						Listen:        sop.Listen,
						SMPPVersion:   sop.SMPPVersion,
						Accounts:      accts,
						BindTypes:     bt,
						MaxBinds:      sop.MaxBinds,
						WindowSize:    sop.WindowSize,
						RateLimited:   limited,
						ThrottleTPS:   tps,
						ThrottleBurst: int(burst),
						ActiveBinds:   activeBinds,
						DLREnabled:    sop.DLR.IsEnabled(),
						MessagesSeen:  msgsSeen,
					})
				}
				writeJSON(w, http.StatusOK, out)
				return
			}
		}

		out := make([]smsc.Snapshot, 0, len(srv.Operators()))
		for _, op := range srv.Operators() {
			out = append(out, op.Describe())
		}
		writeJSON(w, http.StatusOK, out)
	})

	mux.HandleFunc("POST /admin/operators", func(w http.ResponseWriter, r *http.Request) {
		var op config.Operator
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&op); err != nil {
			writeJSON(w, http.StatusBadRequest, errBody{"invalid JSON body: " + err.Error()})
			return
		}
		if op.Name == "" || op.Listen == "" {
			writeJSON(w, http.StatusBadRequest, errBody{"name and listen address are required"})
			return
		}
		if op.SMPPVersion == "" {
			op.SMPPVersion = "3.4"
		}
		if len(op.Accounts) == 0 {
			writeJSON(w, http.StatusBadRequest, errBody{"at least one account (system_id and password) is required"})
			return
		}

		if st != nil {
			if err := st.CreateOperator(op); err != nil {
				writeJSON(w, http.StatusConflict, errBody{"database error: " + err.Error()})
				return
			}
		}

		if err := srv.AddOperator(op); err != nil {
			if st != nil {
				_ = st.DeleteOperator(op.Name)
			}
			writeJSON(w, http.StatusInternalServerError, errBody{"failed to start operator: " + err.Error()})
			return
		}

		log.Info("admin: created operator", "name", op.Name, "listen", op.Listen)
		writeJSON(w, http.StatusCreated, map[string]string{"status": "created", "name": op.Name})
	})

	mux.HandleFunc("GET /admin/operators/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if st != nil {
			op, err := st.GetOperator(name)
			if err != nil {
				writeJSON(w, http.StatusNotFound, errBody{err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, op)
			return
		}

		op := srv.OperatorByName(name)
		if op == nil {
			writeJSON(w, http.StatusNotFound, errBody{"operator not found"})
			return
		}
		writeJSON(w, http.StatusOK, op.Config())
	})

	mux.HandleFunc("PUT /admin/operators/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		var op config.Operator
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&op); err != nil {
			writeJSON(w, http.StatusBadRequest, errBody{"invalid JSON body: " + err.Error()})
			return
		}
		if op.Name == "" {
			op.Name = name
		}
		if op.Listen == "" {
			writeJSON(w, http.StatusBadRequest, errBody{"listen address is required"})
			return
		}

		if st != nil {
			if err := st.UpdateOperator(name, op); err != nil {
				writeJSON(w, http.StatusInternalServerError, errBody{"database update error: " + err.Error()})
				return
			}
		}

		if err := srv.UpdateOperator(name, op); err != nil {
			writeJSON(w, http.StatusInternalServerError, errBody{"runtime update error: " + err.Error()})
			return
		}

		log.Info("admin: updated operator", "name", name, "newName", op.Name, "listen", op.Listen)
		writeJSON(w, http.StatusOK, map[string]string{"status": "updated", "name": op.Name})
	})

	mux.HandleFunc("DELETE /admin/operators/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if st != nil {
			if err := st.DeleteOperator(name); err != nil {
				writeJSON(w, http.StatusNotFound, errBody{"database delete error: " + err.Error()})
				return
			}
		}

		if err := srv.DeleteOperator(name); err != nil {
			writeJSON(w, http.StatusNotFound, errBody{"delete error: " + err.Error()})
			return
		}

		log.Info("admin: deleted operator", "name", name)
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "name": name})
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
