package dlr

import (
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/dilshodgayibnazarov/smsc-sim/internal/config"
	"github.com/dilshodgayibnazarov/smsc-sim/internal/smpp"
)

func TestPickRespectsWeights(t *testing.T) {
	e := New(config.DLR{
		Outcomes: map[string]int{"DELIVRD": 80, "UNDELIV": 20},
		ErrCodes: map[string]int{"UNDELIV": 1282},
		Template: config.DefaultReceiptTemplate,
	})
	rng := rand.New(rand.NewSource(1))
	var delivrd, undeliv int
	for i := 0; i < 10000; i++ {
		o := e.Pick(rng)
		switch o.Stat {
		case "DELIVRD":
			delivrd++
		case "UNDELIV":
			undeliv++
			if o.ErrCode != 1282 {
				t.Fatalf("UNDELIV err code = %d, want 1282", o.ErrCode)
			}
			if o.State != smpp.StateUndeliverable {
				t.Fatalf("UNDELIV state = %d", o.State)
			}
		default:
			t.Fatalf("unexpected stat %q", o.Stat)
		}
	}
	ratio := float64(delivrd) / float64(delivrd+undeliv)
	if ratio < 0.76 || ratio > 0.84 {
		t.Fatalf("DELIVRD ratio = %.3f, want ~0.80", ratio)
	}
}

func TestBuildReceiptText(t *testing.T) {
	e := New(config.DLR{
		Outcomes: map[string]int{"DELIVRD": 1},
		Template: config.DefaultReceiptTemplate,
		TLV:      true,
	})
	orig := &smpp.SM{SourceAddr: "3700", DestAddr: "998901112233", ShortMessage: []byte("hello there this is a long message")}
	sm := e.Build(orig, "DEADBEEF", Outcome{Stat: "DELIVRD", State: smpp.StateDelivered}, time.Unix(1700000000, 0), time.Unix(1700000030, 0))

	txt := string(sm.ShortMessage)
	for _, want := range []string{"id:DEADBEEF", "stat:DELIVRD", "err:000", "dlvrd:001", "text:hello there this is "} {
		if !strings.Contains(txt, want) {
			t.Errorf("receipt %q missing %q", txt, want)
		}
	}
	if sm.SourceAddr != "998901112233" || sm.DestAddr != "3700" {
		t.Errorf("receipt addresses not swapped: src=%s dst=%s", sm.SourceAddr, sm.DestAddr)
	}
	if sm.ESMClass&smpp.ESMClassDeliveryReceipt == 0 {
		t.Errorf("esm_class missing delivery-receipt bit: %#x", sm.ESMClass)
	}
	if _, ok := sm.TLV(smpp.TagReceiptedMessageID); !ok {
		t.Error("receipted_message_id TLV not present")
	}
	if v, ok := sm.TLV(smpp.TagMessageStateOption); !ok || v[0] != smpp.StateDelivered {
		t.Errorf("message_state TLV = %v ok=%v", v, ok)
	}
}

func TestDisabledEngine(t *testing.T) {
	no := false
	if New(config.DLR{Enabled: &no, Outcomes: map[string]int{"DELIVRD": 1}}).Enabled() {
		t.Error("engine should be disabled")
	}
	if New(config.DLR{}).Enabled() {
		t.Error("engine with no outcomes should be disabled")
	}
}
