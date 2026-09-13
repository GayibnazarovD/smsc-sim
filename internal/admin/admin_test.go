package admin

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/dilshodgayibnazarov/smsc-sim/internal/config"
	"github.com/dilshodgayibnazarov/smsc-sim/internal/metrics"
	"github.com/dilshodgayibnazarov/smsc-sim/internal/smsc"
)

func setupTestServer(t *testing.T) (*smsc.Server, http.Handler) {
	t.Helper()
	cfg := &config.Config{
		Seed: 42,
		Operators: []config.Operator{
			{
				Name:        "test-op",
				Listen:      "127.0.0.1:0",
				SMPPVersion: "3.4",
				Accounts: []config.Account{
					{SystemID: "testuser", Password: "secret"},
				},
				Throttle: config.Throttle{TPS: 100, Burst: 100},
			},
		},
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	reg := prometheus.NewRegistry()
	m := metrics.New(reg)
	srv := smsc.New(cfg, log, m)
	h := Handler(srv, log)
	return srv, h
}

func TestWebUIRoutes(t *testing.T) {
	_, h := setupTestServer(t)

	// Test GET / (index.html)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET / expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "smsc-sim | Fleet Admin Dashboard") {
		t.Fatalf("GET / does not contain expected title: %s", w.Body.String()[:100])
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("GET / expected text/html content-type, got %s", ct)
	}

	// Test GET /ui/style.css
	reqCSS := httptest.NewRequest(http.MethodGet, "/ui/style.css", nil)
	wCSS := httptest.NewRecorder()
	h.ServeHTTP(wCSS, reqCSS)
	if wCSS.Code != http.StatusOK {
		t.Fatalf("GET /ui/style.css expected 200, got %d", wCSS.Code)
	}

	// Test GET /ui/app.js
	reqJS := httptest.NewRequest(http.MethodGet, "/ui/app.js", nil)
	wJS := httptest.NewRecorder()
	h.ServeHTTP(wJS, reqJS)
	if wJS.Code != http.StatusOK {
		t.Fatalf("GET /ui/app.js expected 200, got %d", wJS.Code)
	}
}

func TestAdminAPIRoutes(t *testing.T) {
	srv, h := setupTestServer(t)

	// Healthz
	reqHealth := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	wHealth := httptest.NewRecorder()
	h.ServeHTTP(wHealth, reqHealth)
	if wHealth.Code != http.StatusOK || wHealth.Body.String() != "ok" {
		t.Fatalf("healthz failed: %d %s", wHealth.Code, wHealth.Body.String())
	}

	// Overview
	reqOv := httptest.NewRequest(http.MethodGet, "/admin/overview", nil)
	wOv := httptest.NewRecorder()
	h.ServeHTTP(wOv, reqOv)
	if wOv.Code != http.StatusOK {
		t.Fatalf("overview failed: %d", wOv.Code)
	}
	var ov OverviewSnapshot
	if err := json.NewDecoder(wOv.Body).Decode(&ov); err != nil {
		t.Fatalf("decode overview json: %v", err)
	}
	if ov.OperatorsCount != 1 {
		t.Errorf("expected 1 operator, got %d", ov.OperatorsCount)
	}
	if ov.ThrottledOperators != 1 {
		t.Errorf("expected 1 throttled operator, got %d", ov.ThrottledOperators)
	}

	// Operators
	reqOps := httptest.NewRequest(http.MethodGet, "/admin/operators", nil)
	wOps := httptest.NewRecorder()
	h.ServeHTTP(wOps, reqOps)
	if wOps.Code != http.StatusOK {
		t.Fatalf("operators failed: %d", wOps.Code)
	}
	var ops []smsc.Snapshot
	if err := json.NewDecoder(wOps.Body).Decode(&ops); err != nil {
		t.Fatalf("decode operators: %v", err)
	}
	if len(ops) != 1 || ops[0].Name != "test-op" {
		t.Fatalf("unexpected operators response: %+v", ops)
	}

	// Sessions (empty initially)
	reqSess := httptest.NewRequest(http.MethodGet, "/admin/sessions", nil)
	wSess := httptest.NewRecorder()
	h.ServeHTTP(wSess, reqSess)
	if wSess.Code != http.StatusOK {
		t.Fatalf("sessions failed: %d", wSess.Code)
	}

	// Disconnect unknown session
	reqDisc := httptest.NewRequest(http.MethodPost, "/admin/sessions/unknown-id/disconnect", nil)
	wDisc := httptest.NewRecorder()
	h.ServeHTTP(wDisc, reqDisc)
	if wDisc.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown session disconnect, got %d", wDisc.Code)
	}

	// Events
	srv.RecordEvent("test-op", "bind", "info", "Test bind event", "remote=127.0.0.1")
	reqEv := httptest.NewRequest(http.MethodGet, "/admin/events?limit=10", nil)
	wEv := httptest.NewRecorder()
	h.ServeHTTP(wEv, reqEv)
	if wEv.Code != http.StatusOK {
		t.Fatalf("events failed: %d", wEv.Code)
	}
	var evs []smsc.Event
	if err := json.NewDecoder(wEv.Body).Decode(&evs); err != nil {
		t.Fatalf("decode events: %v", err)
	}
	if len(evs) != 1 || evs[0].Message != "Test bind event" {
		t.Fatalf("unexpected events: %+v", evs)
	}

	// Config
	reqCfg := httptest.NewRequest(http.MethodGet, "/admin/config", nil)
	wCfg := httptest.NewRecorder()
	h.ServeHTTP(wCfg, reqCfg)
	if wCfg.Code != http.StatusOK {
		t.Fatalf("config failed: %d", wCfg.Code)
	}

	// MO injection without receiver fails with 409
	moBody := strings.NewReader(`{"source":"123","dest":"456","text":"hello"}`)
	reqMO := httptest.NewRequest(http.MethodPost, "/admin/operators/test-op/mo", moBody)
	wMO := httptest.NewRecorder()
	h.ServeHTTP(wMO, reqMO)
	if wMO.Code != http.StatusConflict {
		t.Fatalf("expected 409 for MO with no receiver, got %d", wMO.Code)
	}
}

func TestFormatDuration(t *testing.T) {
	if got := formatDuration(45 * time.Second); got != "45s" {
		t.Errorf("expected 45s, got %s", got)
	}
	if got := formatDuration(2*time.Minute + 15*time.Second); got != "2m 15s" {
		t.Errorf("expected 2m 15s, got %s", got)
	}
	if got := formatDuration(3*time.Hour + 4*time.Minute + 5*time.Second); got != "3h 04m 05s" {
		t.Errorf("expected 3h 04m 05s, got %s", got)
	}
}
