package smsc

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/dilshodgayibnazarov/smsc-sim/internal/config"
	"github.com/dilshodgayibnazarov/smsc-sim/internal/metrics"
	"github.com/dilshodgayibnazarov/smsc-sim/internal/smpp"
)

// testClient is a minimal SMPP ESME used only by these tests.
type testClient struct {
	conn net.Conn
	br   *bufio.Reader
	seq  uint32
}

func dial(t *testing.T, addr string) *testClient {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return &testClient{conn: c, br: bufio.NewReader(c)}
}

func (c *testClient) send(t *testing.T, id smpp.CommandID, body []byte) uint32 {
	t.Helper()
	c.seq++
	if _, err := c.conn.Write(smpp.Marshal(id, smpp.ESME_ROK, c.seq, body)); err != nil {
		t.Fatalf("write %s: %v", id, err)
	}
	return c.seq
}

func (c *testClient) read(t *testing.T, within time.Duration) *smpp.RawPDU {
	t.Helper()
	_ = c.conn.SetReadDeadline(time.Now().Add(within))
	p, err := smpp.ReadRaw(c.br)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return p
}

// readOf reads until a PDU with the wanted command id arrives (skipping e.g.
// server enquire_link), or fails after the deadline.
func (c *testClient) readOf(t *testing.T, want smpp.CommandID, within time.Duration) *smpp.RawPDU {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		_ = c.conn.SetReadDeadline(deadline)
		p, err := smpp.ReadRaw(c.br)
		if err != nil {
			t.Fatalf("read waiting for %s: %v", want, err)
		}
		if p.Header.ID == want {
			return p
		}
	}
	t.Fatalf("timed out waiting for %s", want)
	return nil
}

func (c *testClient) bind(t *testing.T, sysID, pass string) *smpp.RawPDU {
	t.Helper()
	body := (&smpp.Bind{SystemID: sysID, Password: pass, SystemType: "", InterfaceVersion: smpp.Version34}).Encode()
	c.send(t, smpp.BindTransceiver, body)
	return c.readOf(t, smpp.BindTransceiverResp, 2*time.Second)
}

func (c *testClient) submit(t *testing.T, src, dst, text string, wantDLR bool) uint32 {
	t.Helper()
	rd := uint8(0)
	if wantDLR {
		rd = 1
	}
	sm := &smpp.SM{SourceAddr: src, DestAddr: dst, RegisteredDelivery: rd, ShortMessage: []byte(text)}
	return c.send(t, smpp.SubmitSM, sm.Encode())
}

func startServer(t *testing.T, ops []config.Operator) *Server {
	t.Helper()
	cfg := &config.Config{Seed: 1, Operators: ops}
	// mimic what config.Parse would have filled in
	for i := range cfg.Operators {
		if cfg.Operators[i].SMPPVersion == "" {
			cfg.Operators[i].SMPPVersion = "3.4"
		}
		if cfg.Operators[i].SubmitRespLatency.Dist == "" {
			cfg.Operators[i].SubmitRespLatency.Dist = "fixed"
		}
		if cfg.Operators[i].DLR.Template == "" {
			cfg.Operators[i].DLR.Template = config.DefaultReceiptTemplate
		}
	}
	m := metrics.New(prometheus.NewRegistry())
	srv := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), m)
	if err := srv.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	return srv
}

func op(name string) config.Operator {
	return config.Operator{
		Name:     name,
		Listen:   "127.0.0.1:0",
		Accounts: []config.Account{{SystemID: "esme", Password: "pw"}},
		DLR:      config.DLR{Outcomes: map[string]int{"DELIVRD": 1}},
	}
}

func TestBindSubmitAndDLR(t *testing.T) {
	o := op("beeline")
	o.DLR.Delay = config.Range{} // immediate receipt
	srv := startServer(t, []config.Operator{o})
	addr := srv.Operators()[0].Addr()

	c := dial(t, addr)
	if resp := c.bind(t, "esme", "pw"); resp.Header.Status != smpp.ESME_ROK {
		t.Fatalf("bind status = %#x, want ESME_ROK", resp.Header.Status)
	}

	seq := c.submit(t, "3700", "998900000001", "hello", true)
	sr := c.readOf(t, smpp.SubmitSMResp, 2*time.Second)
	if sr.Header.Status != smpp.ESME_ROK {
		t.Fatalf("submit_sm_resp status = %#x", sr.Header.Status)
	}
	if sr.Header.Seq != seq {
		t.Errorf("submit_sm_resp seq = %d, want %d", sr.Header.Seq, seq)
	}
	msgID := smpp.DecodeMessageID(sr.Body)
	if msgID == "" {
		t.Fatal("empty message_id in submit_sm_resp")
	}

	dlr := c.readOf(t, smpp.DeliverSM, 2*time.Second)
	sm, err := smpp.DecodeSM(dlr.Body)
	if err != nil {
		t.Fatalf("decode DLR: %v", err)
	}
	if sm.ESMClass&smpp.ESMClassDeliveryReceipt == 0 {
		t.Errorf("DLR esm_class = %#x, missing receipt bit", sm.ESMClass)
	}
	if got := string(sm.ShortMessage); !contains(got, "id:"+msgID) || !contains(got, "stat:DELIVRD") {
		t.Errorf("DLR text = %q, want id:%s and stat:DELIVRD", got, msgID)
	}
	// ack it
	c.send(t, smpp.DeliverSMResp, smpp.EncodeSubmitSMResp(msgID))
}

func TestBadPasswordRejected(t *testing.T) {
	srv := startServer(t, []config.Operator{op("ucell")})
	c := dial(t, srv.Operators()[0].Addr())
	resp := c.bind(t, "esme", "wrong")
	if resp.Header.Status != smpp.ESME_RINVPASWD {
		t.Fatalf("bind status = %#x, want ESME_RINVPASWD", resp.Header.Status)
	}
}

func TestUnknownSystemIDRejected(t *testing.T) {
	srv := startServer(t, []config.Operator{op("ucell")})
	c := dial(t, srv.Operators()[0].Addr())
	resp := c.bind(t, "nobody", "pw")
	if resp.Header.Status != smpp.ESME_RINVSYSID {
		t.Fatalf("bind status = %#x, want ESME_RINVSYSID", resp.Header.Status)
	}
}

func TestThrottleReturnsRThrottled(t *testing.T) {
	o := op("mobiuz")
	o.Throttle = config.Throttle{TPS: 5, Burst: 5}
	o.DLR.Enabled = ptr(false)
	srv := startServer(t, []config.Operator{o})
	c := dial(t, srv.Operators()[0].Addr())
	if c.bind(t, "esme", "pw").Header.Status != smpp.ESME_ROK {
		t.Fatal("bind failed")
	}

	var ok, throttled int
	for i := 0; i < 40; i++ {
		c.submit(t, "3700", "998900000002", "x", false)
	}
	for i := 0; i < 40; i++ {
		r := c.readOf(t, smpp.SubmitSMResp, 2*time.Second)
		switch r.Header.Status {
		case smpp.ESME_ROK:
			ok++
		case smpp.ESME_RTHROTTLED:
			throttled++
		default:
			t.Fatalf("unexpected submit_sm_resp status %#x", r.Header.Status)
		}
	}
	if ok == 0 || throttled == 0 {
		t.Fatalf("expected a mix of accepted and throttled, got ok=%d throttled=%d", ok, throttled)
	}
	if ok > 12 {
		t.Fatalf("token bucket leaked: %d accepted from a burst of 5 @ 5tps", ok)
	}
}

func TestSubmitBeforeBindRejected(t *testing.T) {
	srv := startServer(t, []config.Operator{op("beeline")})
	c := dial(t, srv.Operators()[0].Addr())
	c.submit(t, "3700", "998900000003", "x", false)
	r := c.readOf(t, smpp.SubmitSMResp, 2*time.Second)
	if r.Header.Status != smpp.ESME_RINVBNDSTS {
		t.Fatalf("status = %#x, want ESME_RINVBNDSTS", r.Header.Status)
	}
}

func TestUnbind(t *testing.T) {
	srv := startServer(t, []config.Operator{op("beeline")})
	c := dial(t, srv.Operators()[0].Addr())
	c.bind(t, "esme", "pw")
	c.send(t, smpp.Unbind, nil)
	r := c.readOf(t, smpp.UnbindResp, 2*time.Second)
	if r.Header.Status != smpp.ESME_ROK {
		t.Fatalf("unbind_resp status = %#x", r.Header.Status)
	}
}

func TestMOInjection(t *testing.T) {
	o := op("humans")
	srv := startServer(t, []config.Operator{o})
	c := dial(t, srv.Operators()[0].Addr())
	c.bind(t, "esme", "pw")
	// let the bind register
	time.Sleep(50 * time.Millisecond)

	if err := srv.InjectMO("humans", "998901234567", "3700", "inbound hi", 0); err != nil {
		t.Fatalf("InjectMO: %v", err)
	}
	mo := c.readOf(t, smpp.DeliverSM, 2*time.Second)
	sm, err := smpp.DecodeSM(mo.Body)
	if err != nil {
		t.Fatalf("decode MO: %v", err)
	}
	if string(sm.ShortMessage) != "inbound hi" || sm.SourceAddr != "998901234567" {
		t.Fatalf("unexpected MO: %+v", sm)
	}
}

func ptr[T any](v T) *T { return &v }

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestDynamicOperatorLifecycle(t *testing.T) {
	o1 := op("op1")
	srv := startServer(t, []config.Operator{o1})

	// Add second operator dynamically
	o2 := op("op2")
	o2.Listen = "127.0.0.1:0"
	if err := srv.AddOperator(o2); err != nil {
		t.Fatalf("AddOperator: %v", err)
	}
	if len(srv.Operators()) != 2 {
		t.Fatalf("expected 2 operators, got %d", len(srv.Operators()))
	}

	target := srv.OperatorByName("op2")
	if target == nil {
		t.Fatalf("expected op2 to exist")
	}

	// Dial op2
	c := dial(t, target.Addr())
	c.bind(t, "esme", "pw")
	time.Sleep(30 * time.Millisecond)
	if target.Describe().ActiveBinds != 1 {
		t.Fatalf("expected 1 active bind on op2, got %d", target.Describe().ActiveBinds)
	}

	// Update op2 rate limit
	o2Updated := o2
	o2Updated.Throttle = config.Throttle{TPS: 300, Burst: 300}
	if err := srv.UpdateOperator("op2", o2Updated); err != nil {
		t.Fatalf("UpdateOperator: %v", err)
	}
	if srv.OperatorByName("op2").Describe().ThrottleTPS != 300 {
		t.Fatalf("expected TPS to be 300")
	}

	// Delete op2
	if err := srv.DeleteOperator("op2"); err != nil {
		t.Fatalf("DeleteOperator: %v", err)
	}
	if len(srv.Operators()) != 1 {
		t.Fatalf("expected 1 operator after delete, got %d", len(srv.Operators()))
	}
}

