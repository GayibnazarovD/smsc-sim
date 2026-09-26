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
	"github.com/dilshodgayibnazarov/smsc-sim/internal/store"
)

func setupTestServer(t *testing.T) (*smsc.Server, *store.Store, http.Handler, string) {
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
	if err := srv.Start(); err != nil {
		t.Fatalf("start srv: %v", err)
	}

	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	_ = st.SeedIfEmpty(cfg.Operators)

	// Create test admin user and session
	u, err := st.CreateUser("admin", "adminpass123", "admin")
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}
	sess, err := st.CreateSession(u, 24*time.Hour)
	if err != nil {
		t.Fatalf("create test session: %v", err)
	}

	h := Handler(srv, st, log)
	return srv, st, h, sess.Token
}

func authedReq(method, path, token string, body io.Reader) *http.Request {
	req := httptest.NewRequest(method, path, body)
	if token != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	}
	return req
}

func TestWebUIRoutes(t *testing.T) {
	_, _, h, _ := setupTestServer(t)

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

func TestAuthEndpoints(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{Seed: 42}
	srv := smsc.New(cfg, log, metrics.New(prometheus.NewRegistry()))
	_ = srv.Start()

	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	h := Handler(srv, st, log)

	// 1. Initial status: uninitialized
	reqStatus := httptest.NewRequest(http.MethodGet, "/admin/auth/status", nil)
	wStatus := httptest.NewRecorder()
	h.ServeHTTP(wStatus, reqStatus)
	if wStatus.Code != http.StatusOK {
		t.Fatalf("auth status expected 200, got %d", wStatus.Code)
	}
	var stResp authStatusResp
	_ = json.NewDecoder(wStatus.Body).Decode(&stResp)
	if stResp.Initialized || stResp.Authenticated {
		t.Fatalf("expected uninitialized system: %+v", stResp)
	}

	// 2. Unauthenticated access to /admin/overview rejected with 401 setup_required
	reqOv := httptest.NewRequest(http.MethodGet, "/admin/overview", nil)
	wOv := httptest.NewRecorder()
	h.ServeHTTP(wOv, reqOv)
	if wOv.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 unauthorized, got %d", wOv.Code)
	}
	if !strings.Contains(wOv.Body.String(), "setup_required") {
		t.Fatalf("expected setup_required error, got: %s", wOv.Body.String())
	}

	// 3. Setup first user
	setupBody := `{"username": "admin", "password": "masterpassword123", "confirm_password": "masterpassword123"}`
	reqSetup := httptest.NewRequest(http.MethodPost, "/admin/auth/setup", strings.NewReader(setupBody))
	wSetup := httptest.NewRecorder()
	h.ServeHTTP(wSetup, reqSetup)
	if wSetup.Code != http.StatusCreated {
		t.Fatalf("setup expected 201, got %d: %s", wSetup.Code, wSetup.Body.String())
	}
	cookies := wSetup.Result().Cookies()
	var sessionToken string
	for _, c := range cookies {
		if c.Name == sessionCookieName {
			sessionToken = c.Value
		}
	}
	if sessionToken == "" {
		t.Fatalf("expected session cookie to be set")
	}

	// 4. Repeated setup should be forbidden (403)
	reqSetup2 := httptest.NewRequest(http.MethodPost, "/admin/auth/setup", strings.NewReader(setupBody))
	wSetup2 := httptest.NewRecorder()
	h.ServeHTTP(wSetup2, reqSetup2)
	if wSetup2.Code != http.StatusForbidden {
		t.Fatalf("repeated setup expected 403, got %d", wSetup2.Code)
	}

	// 5. Check status with session token
	reqStatusAuthed := httptest.NewRequest(http.MethodGet, "/admin/auth/status", nil)
	reqStatusAuthed.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sessionToken})
	wStatusAuthed := httptest.NewRecorder()
	h.ServeHTTP(wStatusAuthed, reqStatusAuthed)
	if wStatusAuthed.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", wStatusAuthed.Code)
	}
	var stAuthed authStatusResp
	_ = json.NewDecoder(wStatusAuthed.Body).Decode(&stAuthed)
	if !stAuthed.Initialized || !stAuthed.Authenticated || stAuthed.User.Username != "admin" {
		t.Fatalf("expected authed status, got %+v", stAuthed)
	}

	// 6. Login endpoint
	loginBody := `{"username": "admin", "password": "masterpassword123"}`
	reqLogin := httptest.NewRequest(http.MethodPost, "/admin/auth/login", strings.NewReader(loginBody))
	wLogin := httptest.NewRecorder()
	h.ServeHTTP(wLogin, reqLogin)
	if wLogin.Code != http.StatusOK {
		t.Fatalf("login expected 200, got %d: %s", wLogin.Code, wLogin.Body.String())
	}

	// 7. Change password
	changeBody := `{"old_password": "masterpassword123", "new_password": "newpassword456", "confirm_password": "newpassword456"}`
	reqChange := authedReq(http.MethodPost, "/admin/auth/change-password", sessionToken, strings.NewReader(changeBody))
	wChange := httptest.NewRecorder()
	h.ServeHTTP(wChange, reqChange)
	if wChange.Code != http.StatusOK {
		t.Fatalf("change password expected 200, got %d: %s", wChange.Code, wChange.Body.String())
	}

	// 8. Logout
	reqLogout := authedReq(http.MethodPost, "/admin/auth/logout", sessionToken, nil)
	wLogout := httptest.NewRecorder()
	h.ServeHTTP(wLogout, reqLogout)
	if wLogout.Code != http.StatusOK {
		t.Fatalf("logout expected 200, got %d", wLogout.Code)
	}
}

func TestAdminAPIRoutes(t *testing.T) {
	srv, _, h, token := setupTestServer(t)

	// Healthz (open)
	reqHealth := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	wHealth := httptest.NewRecorder()
	h.ServeHTTP(wHealth, reqHealth)
	if wHealth.Code != http.StatusOK || wHealth.Body.String() != "ok" {
		t.Fatalf("healthz failed: %d %s", wHealth.Code, wHealth.Body.String())
	}

	// Overview
	reqOv := authedReq(http.MethodGet, "/admin/overview", token, nil)
	wOv := httptest.NewRecorder()
	h.ServeHTTP(wOv, reqOv)
	if wOv.Code != http.StatusOK {
		t.Fatalf("overview failed: %d %s", wOv.Code, wOv.Body.String())
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
	reqOps := authedReq(http.MethodGet, "/admin/operators", token, nil)
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
	reqSess := authedReq(http.MethodGet, "/admin/sessions", token, nil)
	wSess := httptest.NewRecorder()
	h.ServeHTTP(wSess, reqSess)
	if wSess.Code != http.StatusOK {
		t.Fatalf("sessions failed: %d", wSess.Code)
	}

	// Disconnect unknown session
	reqDisc := authedReq(http.MethodPost, "/admin/sessions/unknown-id/disconnect", token, nil)
	wDisc := httptest.NewRecorder()
	h.ServeHTTP(wDisc, reqDisc)
	if wDisc.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown session disconnect, got %d", wDisc.Code)
	}

	// Events
	srv.RecordEvent("test-op", "bind", "info", "Test bind event", "remote=127.0.0.1")
	reqEv := authedReq(http.MethodGet, "/admin/events?limit=10", token, nil)
	wEv := httptest.NewRecorder()
	h.ServeHTTP(wEv, reqEv)
	if wEv.Code != http.StatusOK {
		t.Fatalf("events failed: %d", wEv.Code)
	}
	var evs []smsc.Event
	if err := json.NewDecoder(wEv.Body).Decode(&evs); err != nil {
		t.Fatalf("decode events: %v", err)
	}
	if len(evs) < 1 || evs[0].Message != "Test bind event" {
		t.Fatalf("unexpected events: %+v", evs)
	}

	// Config
	reqCfg := authedReq(http.MethodGet, "/admin/config", token, nil)
	wCfg := httptest.NewRecorder()
	h.ServeHTTP(wCfg, reqCfg)
	if wCfg.Code != http.StatusOK {
		t.Fatalf("config failed: %d", wCfg.Code)
	}

	// MO injection without receiver fails with 409
	moBody := strings.NewReader(`{"source":"123","dest":"456","text":"hello"}`)
	reqMO := authedReq(http.MethodPost, "/admin/operators/test-op/mo", token, moBody)
	wMO := httptest.NewRecorder()
	h.ServeHTTP(wMO, reqMO)
	if wMO.Code != http.StatusConflict {
		t.Fatalf("expected 409 for MO with no receiver, got %d", wMO.Code)
	}
}

func TestOperatorCRUD(t *testing.T) {
	_, _, h, token := setupTestServer(t)

	// POST /admin/operators
	body := `{
		"name": "carrier-x",
		"listen": "127.0.0.1:0",
		"smpp_version": "3.4",
		"accounts": [{"system_id": "cx_user", "password": "cx_password"}],
		"throttle": {"tps": 200, "burst": 200}
	}`
	reqCreate := authedReq(http.MethodPost, "/admin/operators", token, strings.NewReader(body))
	wCreate := httptest.NewRecorder()
	h.ServeHTTP(wCreate, reqCreate)

	if wCreate.Code != http.StatusCreated {
		t.Fatalf("create operator failed: %d body: %s", wCreate.Code, wCreate.Body.String())
	}

	// GET /admin/operators/carrier-x
	reqGet := authedReq(http.MethodGet, "/admin/operators/carrier-x", token, nil)
	wGet := httptest.NewRecorder()
	h.ServeHTTP(wGet, reqGet)
	if wGet.Code != http.StatusOK {
		t.Fatalf("get operator failed: %d", wGet.Code)
	}
	var op config.Operator
	if err := json.NewDecoder(wGet.Body).Decode(&op); err != nil {
		t.Fatalf("decode op: %v", err)
	}
	if op.Name != "carrier-x" || len(op.Accounts) != 1 {
		t.Fatalf("unexpected op: %+v", op)
	}

	// PUT /admin/operators/carrier-x
	updateBody := `{
		"name": "carrier-x",
		"listen": "127.0.0.1:0",
		"smpp_version": "3.4",
		"accounts": [{"system_id": "cx_user", "password": "new_password"}],
		"throttle": {"tps": 500, "burst": 500}
	}`
	reqPut := authedReq(http.MethodPut, "/admin/operators/carrier-x", token, strings.NewReader(updateBody))
	wPut := httptest.NewRecorder()
	h.ServeHTTP(wPut, reqPut)
	if wPut.Code != http.StatusOK {
		t.Fatalf("update operator failed: %d body: %s", wPut.Code, wPut.Body.String())
	}

	// DELETE /admin/operators/carrier-x
	reqDel := authedReq(http.MethodDelete, "/admin/operators/carrier-x", token, nil)
	wDel := httptest.NewRecorder()
	h.ServeHTTP(wDel, reqDel)
	if wDel.Code != http.StatusOK {
		t.Fatalf("delete operator failed: %d", wDel.Code)
	}

	// GET after delete should 404
	reqGet404 := authedReq(http.MethodGet, "/admin/operators/carrier-x", token, nil)
	wGet404 := httptest.NewRecorder()
	h.ServeHTTP(wGet404, reqGet404)
	if wGet404.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", wGet404.Code)
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
