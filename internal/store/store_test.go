package store

import (
	"testing"
	"time"

	"github.com/dilshodgayibnazarov/smsc-sim/internal/config"
)

func TestStoreCRUD(t *testing.T) {
	st, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open :memory: %v", err)
	}
	defer st.Close()

	// Initial empty list
	list, err := st.ListOperators()
	if err != nil {
		t.Fatalf("ListOperators: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 operators, got %d", len(list))
	}

	// Create
	enabled := true
	op := config.Operator{
		Name:        "CarrierA",
		Listen:      ":2775",
		SMPPVersion: "3.4",
		Accounts: []config.Account{
			{SystemID: "sys_a", Password: "pw_a"},
		},
		BindTypes:  []string{"tx", "rx", "trx"},
		MaxBinds:   5,
		WindowSize: 10,
		Throttle:   config.Throttle{TPS: 150, Burst: 150},
		SubmitRespLatency: config.Latency{
			Dist: "fixed",
			Mean: config.Duration(40 * time.Millisecond),
		},
		DLR: config.DLR{
			Enabled: &enabled,
			Delay: config.Range{
				Min: config.Duration(2 * time.Second),
				Max: config.Duration(10 * time.Second),
			},
		},
	}

	if err := st.CreateOperator(op); err != nil {
		t.Fatalf("CreateOperator: %v", err)
	}

	// Get
	got, err := st.GetOperator("CarrierA")
	if err != nil {
		t.Fatalf("GetOperator: %v", err)
	}
	if got.Name != "CarrierA" || got.Listen != ":2775" || len(got.Accounts) != 1 {
		t.Fatalf("unexpected got operator: %+v", got)
	}
	if got.Throttle.TPS != 150 {
		t.Errorf("expected 150 TPS, got %v", got.Throttle.TPS)
	}

	// Update
	got.Throttle.TPS = 250
	got.Listen = ":2785"
	if err := st.UpdateOperator("CarrierA", *got); err != nil {
		t.Fatalf("UpdateOperator: %v", err)
	}

	updated, err := st.GetOperator("CarrierA")
	if err != nil {
		t.Fatalf("GetOperator after update: %v", err)
	}
	if updated.Throttle.TPS != 250 || updated.Listen != ":2785" {
		t.Fatalf("updated operator mismatch: %+v", updated)
	}

	// Delete
	if err := st.DeleteOperator("CarrierA"); err != nil {
		t.Fatalf("DeleteOperator: %v", err)
	}

	_, err = st.GetOperator("CarrierA")
	if err == nil {
		t.Fatalf("expected error getting deleted operator, got nil")
	}
}

func TestSeedIfEmpty(t *testing.T) {
	st, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	seedList := []config.Operator{
		{Name: "Op1", Listen: ":2775"},
		{Name: "Op2", Listen: ":2776"},
	}

	if err := st.SeedIfEmpty(seedList); err != nil {
		t.Fatalf("SeedIfEmpty: %v", err)
	}

	ops, err := st.ListOperators()
	if err != nil {
		t.Fatalf("ListOperators: %v", err)
	}
	if len(ops) != 2 {
		t.Fatalf("expected 2 operators after seed, got %d", len(ops))
	}

	// Calling again should not duplicate
	if err := st.SeedIfEmpty([]config.Operator{{Name: "Op3", Listen: ":2777"}}); err != nil {
		t.Fatalf("Second SeedIfEmpty: %v", err)
	}

	ops2, err := st.ListOperators()
	if err != nil {
		t.Fatalf("ListOperators: %v", err)
	}
	if len(ops2) != 2 {
		t.Fatalf("expected still 2 operators, got %d", len(ops2))
	}
}
