package smsc

import (
	"log/slog"
	"context"
	"os"
	"testing"
	"time"

	"github.com/dilshodgayibnazarov/smsc-sim/internal/config"
	"github.com/dilshodgayibnazarov/smsc-sim/internal/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

func TestSubmitTestAndVirtualReceiver(t *testing.T) {
	cfg := &config.Config{
		Operators: []config.Operator{
			{
				Name:        "TestOp",
				Listen:      "127.0.0.1:0",
				Accounts:    []config.Account{{SystemID: "test_id", Password: "test_pw"}},
				SMPPVersion: "3.4",
			},
		},
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	m := metrics.New(prometheus.NewRegistry())
	srv := New(cfg, logger, m)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Shutdown(context.Background())

	// 1. Submit normal MT test SMS
	res, err := srv.SubmitTest(SubmitTestRequest{
		Operator:           "TestOp",
		Source:             "COMPANY",
		Dest:               "+998901234567",
		Text:               "Hello from test client",
		RegisteredDelivery: 0,
	})
	if err != nil {
		t.Fatalf("SubmitTest normal failed: %v", err)
	}
	if res.CommandStatus != "ESME_ROK" {
		t.Fatalf("expected ESME_ROK, got %s", res.CommandStatus)
	}
	if res.SourceTON != 5 { // Alphanumeric
		t.Fatalf("expected SourceTON 5, got %d", res.SourceTON)
	}
	if res.DestTON != 1 { // International E.164
		t.Fatalf("expected DestTON 1, got %d", res.DestTON)
	}
	if res.MessageID == "" {
		t.Fatalf("expected non-empty message ID")
	}

	// 2. Submit simulated throttled error
	resThrottled, err := srv.SubmitTest(SubmitTestRequest{
		Operator: "TestOp",
		Source:   "3700",
		Dest:     "+998901234567",
		Text:     "[ERR_THROTTLED] testing rate limit",
	})
	if err != nil {
		t.Fatalf("SubmitTest throttled failed: %v", err)
	}
	if resThrottled.CommandStatus != "ESME_RTHROTTLED" {
		t.Fatalf("expected ESME_RTHROTTLED, got %s", resThrottled.CommandStatus)
	}

	// 3. Test Virtual Receiver & MO Injection
	if srv.IsVirtualReceiverActive("TestOp") {
		t.Fatalf("expected virtual receiver to not be active initially")
	}
	// Injecting MO before receiver connects should fail
	if err := srv.InjectMO("TestOp", "+998901234567", "3700", "Inbound fail", 0); err == nil {
		t.Fatalf("expected InjectMO to fail with no receiver")
	}

	// Start virtual receiver
	if err := srv.StartVirtualReceiver("TestOp"); err != nil {
		t.Fatalf("StartVirtualReceiver failed: %v", err)
	}
	if !srv.IsVirtualReceiverActive("TestOp") {
		t.Fatalf("expected virtual receiver to be active")
	}

	// Inject MO now - must succeed
	if err := srv.InjectMO("TestOp", "+998901234567", "3700", "Inbound success", 0); err != nil {
		t.Fatalf("InjectMO failed with virtual receiver running: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	msgs := srv.VirtualReceiverMessages("TestOp")
	if len(msgs) == 0 {
		t.Fatalf("expected virtual receiver to capture at least 1 message")
	}
	if msgs[0].Text != "Inbound success" {
		t.Fatalf("expected text 'Inbound success', got %q", msgs[0].Text)
	}

	// Stop virtual receiver
	if err := srv.StopVirtualReceiver("TestOp"); err != nil {
		t.Fatalf("StopVirtualReceiver failed: %v", err)
	}
	if srv.IsVirtualReceiverActive("TestOp") {
		t.Fatalf("expected virtual receiver to be stopped")
	}
}
