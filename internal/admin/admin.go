// Package admin exposes an HTTP API and an embedded Web Admin UI for introspecting
// the simulator, managing client sessions, monitoring throughput, configuring operators,
// and securing access with user authentication.
package admin

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dilshodgayibnazarov/smsc-sim/internal/config"
	"github.com/dilshodgayibnazarov/smsc-sim/internal/smsc"
	"github.com/dilshodgayibnazarov/smsc-sim/internal/store"
)

//go:embed ui/*
var uiFS embed.FS

const sessionCookieName = "smsc_session"

type contextKey string

const sessionContextKey contextKey = "smsc_session"

// OverviewSnapshot provides high-level telemetry and status across all operators.
type OverviewSnapshot struct {
	Uptime              string          `json:"uptime"`
	UptimeSeconds       int64           `json:"uptime_seconds"`
	StartTime           time.Time       `json:"start_time"`
	OperatorsCount      int             `json:"operators_count"`
	ActiveBindsCount    int             `json:"active_binds_count"`
	TotalMessagesSeen   uint64          `json:"total_messages_seen"`
	DLREnabledOperators int             `json:"dlr_enabled_operators"`
	ThrottledOperators  int             `json:"throttled_operators"`
	Operators           []smsc.Snapshot `json:"operators"`
}

type authStatusResp struct {
	Initialized   bool        `json:"initialized"`
	Authenticated bool        `json:"authenticated"`
	User          *store.User `json:"user,omitempty"`
}

// Handler returns the admin API and Web UI mux.
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

	// Helper to extract session token from cookie or Authorization header
	extractToken := func(r *http.Request) string {
		if c, err := r.Cookie(sessionCookieName); err == nil && c.Value != "" {
			return c.Value
		}
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			return strings.TrimPrefix(authHeader, "Bearer ")
		}
		return ""
	}

	// ----------------------------------------------------
	// Authentication Endpoints
	// ----------------------------------------------------

	// GET /admin/auth/status
	mux.HandleFunc("GET /admin/auth/status", func(w http.ResponseWriter, r *http.Request) {
		if st == nil {
			writeJSON(w, http.StatusOK, authStatusResp{
				Initialized:   true,
				Authenticated: true,
				User:          &store.User{Username: "anonymous", Role: "admin"},
			})
			return
		}

		hasUsers, err := st.HasUsers()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errBody{"failed checking users: " + err.Error()})
			return
		}

		if !hasUsers {
			writeJSON(w, http.StatusOK, authStatusResp{
				Initialized:   false,
				Authenticated: false,
			})
			return
		}

		token := extractToken(r)
		sess, err := st.ValidateSession(token)
		if err != nil {
			writeJSON(w, http.StatusOK, authStatusResp{
				Initialized:   true,
				Authenticated: false,
			})
			return
		}

		writeJSON(w, http.StatusOK, authStatusResp{
			Initialized:   true,
			Authenticated: true,
			User: &store.User{
				ID:       sess.UserID,
				Username: sess.Username,
				Role:     sess.Role,
			},
		})
	})

	// POST /admin/auth/setup (First-time deployment setup)
	mux.HandleFunc("POST /admin/auth/setup", func(w http.ResponseWriter, r *http.Request) {
		if st == nil {
			writeJSON(w, http.StatusBadRequest, errBody{"database store not enabled"})
			return
		}

		hasUsers, err := st.HasUsers()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errBody{err.Error()})
			return
		}
		if hasUsers {
			writeJSON(w, http.StatusForbidden, errBody{"initial setup has already been completed"})
			return
		}

		var req struct {
			Username        string `json:"username"`
			Password        string `json:"password"`
			ConfirmPassword string `json:"confirm_password"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14)).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errBody{"invalid JSON: " + err.Error()})
			return
		}

		if req.Username == "" || req.Password == "" {
			writeJSON(w, http.StatusBadRequest, errBody{"username and password are required"})
			return
		}
		if req.Password != req.ConfirmPassword {
			writeJSON(w, http.StatusBadRequest, errBody{"passwords do not match"})
			return
		}

		u, err := st.CreateUser(req.Username, req.Password, "admin")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errBody{err.Error()})
			return
		}

		sess, err := st.CreateSession(u, 24*time.Hour)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errBody{"failed to create session: " + err.Error()})
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     sessionCookieName,
			Value:    sess.Token,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   86400,
		})

		log.Info("admin: initial setup completed, created admin user", "username", u.Username)
		writeJSON(w, http.StatusCreated, map[string]any{
			"status": "ok",
			"user":   u,
			"token":  sess.Token,
		})
	})

	// POST /admin/auth/login
	mux.HandleFunc("POST /admin/auth/login", func(w http.ResponseWriter, r *http.Request) {
		if st == nil {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
			return
		}

		hasUsers, _ := st.HasUsers()
		if !hasUsers {
			writeJSON(w, http.StatusBadRequest, errBody{"system uninitialized, complete first-time setup first"})
			return
		}

		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14)).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errBody{"invalid JSON: " + err.Error()})
			return
		}

		u, err := st.AuthenticateUser(req.Username, req.Password)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, errBody{"invalid username or password"})
			return
		}

		sess, err := st.CreateSession(u, 24*time.Hour)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errBody{"failed to create session: " + err.Error()})
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     sessionCookieName,
			Value:    sess.Token,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   86400,
		})

		log.Info("admin: user logged in", "username", u.Username)
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "ok",
			"user":   u,
			"token":  sess.Token,
		})
	})

	// POST /admin/auth/logout
	mux.HandleFunc("POST /admin/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		if st != nil {
			token := extractToken(r)
			_ = st.DeleteSession(token)
		}

		http.SetCookie(w, &http.Cookie{
			Name:     sessionCookieName,
			Value:    "",
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   -1,
		})

		writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
	})

	// Auth Guard Middleware for Protected Endpoints
	protect := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if st == nil {
				next(w, r)
				return
			}

			hasUsers, err := st.HasUsers()
			if err != nil || !hasUsers {
				writeJSON(w, http.StatusUnauthorized, map[string]string{
					"error":   "setup_required",
					"message": "First-time deployment setup required",
				})
				return
			}

			token := extractToken(r)
			sess, err := st.ValidateSession(token)
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{
					"error":   "unauthorized",
					"message": "Authentication required. Please sign in.",
				})
				return
			}

			ctx := context.WithValue(r.Context(), sessionContextKey, sess)
			next(w, r.WithContext(ctx))
		}
	}

	// POST /admin/auth/change-password (Protected)
	mux.HandleFunc("POST /admin/auth/change-password", protect(func(w http.ResponseWriter, r *http.Request) {
		sess, ok := r.Context().Value(sessionContextKey).(*store.Session)
		if !ok || sess == nil {
			writeJSON(w, http.StatusUnauthorized, errBody{"unauthorized"})
			return
		}

		var req struct {
			OldPassword     string `json:"old_password"`
			NewPassword     string `json:"new_password"`
			ConfirmPassword string `json:"confirm_password"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14)).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errBody{"invalid JSON: " + err.Error()})
			return
		}

		if req.NewPassword != req.ConfirmPassword {
			writeJSON(w, http.StatusBadRequest, errBody{"new passwords do not match"})
			return
		}

		if err := st.ChangePassword(sess.Username, req.OldPassword, req.NewPassword); err != nil {
			writeJSON(w, http.StatusBadRequest, errBody{err.Error()})
			return
		}

		// Re-authenticate and issue fresh session cookie
		u, _ := st.AuthenticateUser(sess.Username, req.NewPassword)
		if u != nil {
			newSess, err := st.CreateSession(u, 24*time.Hour)
			if err == nil {
				http.SetCookie(w, &http.Cookie{
					Name:     sessionCookieName,
					Value:    newSess.Token,
					Path:     "/",
					HttpOnly: true,
					SameSite: http.SameSiteLaxMode,
					MaxAge:   86400,
				})
			}
		}

		log.Info("admin: password changed", "username", sess.Username)
		writeJSON(w, http.StatusOK, map[string]string{"status": "password_changed"})
	}))

	// ----------------------------------------------------
	// Protected Admin Management Endpoints
	// ----------------------------------------------------

	mux.HandleFunc("GET /admin/overview", protect(func(w http.ResponseWriter, _ *http.Request) {
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
	}))

	mux.HandleFunc("GET /admin/operators", protect(func(w http.ResponseWriter, _ *http.Request) {
		if st != nil {
			stored, err := st.ListOperators()
			if err == nil {
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
	}))

	mux.HandleFunc("POST /admin/operators", protect(func(w http.ResponseWriter, r *http.Request) {
		var op config.Operator
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&op); err != nil {
			writeJSON(w, http.StatusBadRequest, errBody{"invalid JSON body: " + err.Error()})
			return
		}
		op.Name = strings.TrimSpace(op.Name)
		op.Listen = strings.TrimSpace(op.Listen)
		if op.Name == "" || op.Listen == "" {
			writeJSON(w, http.StatusBadRequest, errBody{"name and listen address are required"})
			return
		}
		if !strings.Contains(op.Listen, ":") {
			op.Listen = ":" + op.Listen
		}
		if op.SMPPVersion == "" {
			op.SMPPVersion = "3.4"
		}
		if len(op.Accounts) == 0 {
			writeJSON(w, http.StatusBadRequest, errBody{"at least one account (system_id and password) is required"})
			return
		}
		if op.DLR.IsEnabled() && len(op.DLR.Outcomes) == 0 {
			op.DLR.Outcomes = map[string]int{
				"DELIVRD": 92,
				"UNDELIV": 5,
				"EXPIRED": 2,
				"REJECTD": 1,
			}
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
	}))

	mux.HandleFunc("GET /admin/operators/{name}", protect(func(w http.ResponseWriter, r *http.Request) {
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
	}))

	mux.HandleFunc("PUT /admin/operators/{name}", protect(func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		var op config.Operator
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&op); err != nil {
			writeJSON(w, http.StatusBadRequest, errBody{"invalid JSON body: " + err.Error()})
			return
		}
		if op.Name == "" {
			op.Name = name
		}
		op.Name = strings.TrimSpace(op.Name)
		op.Listen = strings.TrimSpace(op.Listen)
		if op.Listen == "" {
			writeJSON(w, http.StatusBadRequest, errBody{"listen address is required"})
			return
		}
		if !strings.Contains(op.Listen, ":") {
			op.Listen = ":" + op.Listen
		}
		if op.DLR.IsEnabled() && len(op.DLR.Outcomes) == 0 {
			op.DLR.Outcomes = map[string]int{
				"DELIVRD": 92,
				"UNDELIV": 5,
				"EXPIRED": 2,
				"REJECTD": 1,
			}
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
	}))

	mux.HandleFunc("DELETE /admin/operators/{name}", protect(func(w http.ResponseWriter, r *http.Request) {
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
	}))

	mux.HandleFunc("GET /admin/sessions", protect(func(w http.ResponseWriter, _ *http.Request) {
		sessions := srv.Sessions()
		writeJSON(w, http.StatusOK, sessions)
	}))

	mux.HandleFunc("POST /admin/sessions/{id}/disconnect", protect(func(w http.ResponseWriter, r *http.Request) {
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
	}))

	mux.HandleFunc("GET /admin/events", protect(func(w http.ResponseWriter, r *http.Request) {
		limit := 100
		if q := r.URL.Query().Get("limit"); q != "" {
			if n, err := strconv.Atoi(q); err == nil && n > 0 {
				limit = n
			}
		}
		events := srv.Events(limit)
		writeJSON(w, http.StatusOK, events)
	}))

	mux.HandleFunc("GET /admin/config", protect(func(w http.ResponseWriter, _ *http.Request) {
		cfg := srv.Config()
		if cfg == nil {
			cfg = &config.Config{}
		}
		writeJSON(w, http.StatusOK, cfg)
	}))

	mux.HandleFunc("POST /admin/operators/{name}/mo", protect(func(w http.ResponseWriter, r *http.Request) {
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
	}))

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
