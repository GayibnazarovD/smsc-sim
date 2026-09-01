// Package admin exposes a small JSON HTTP API for introspecting the simulator
// and injecting mobile-originated messages at runtime.
package admin

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/dilshodgayibnazarov/smsc-sim/internal/smsc"
)

// Handler returns the admin API mux.
//
//	GET  /healthz                       -> 200 "ok"
//	GET  /admin/operators               -> [ { operator snapshot } ]
//	POST /admin/operators/{name}/mo     -> inject a mobile-originated message
func Handler(srv *smsc.Server, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("GET /admin/operators", func(w http.ResponseWriter, _ *http.Request) {
		out := make([]smsc.Snapshot, 0, len(srv.Operators()))
		for _, op := range srv.Operators() {
			out = append(out, op.Describe())
		}
		writeJSON(w, http.StatusOK, out)
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
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "sent"})
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
