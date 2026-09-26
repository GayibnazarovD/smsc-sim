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

func TestStoreUsersAndSessions(t *testing.T) {
	st, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open :memory: %v", err)
	}
	defer st.Close()

	// Initial: no users
	has, err := st.HasUsers()
	if err != nil {
		t.Fatalf("HasUsers: %v", err)
	}
	if has {
		t.Fatalf("expected HasUsers to be false initially")
	}

	// Invalid user validation
	if _, err := st.CreateUser("ab", "pass1234", "admin"); err == nil {
		t.Fatalf("expected error for short username")
	}
	if _, err := st.CreateUser("valid_user", "short", "admin"); err == nil {
		t.Fatalf("expected error for short password")
	}

	// Create valid user
	u, err := st.CreateUser("admin", "supersecret123", "admin")
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	if u.Username != "admin" || u.Role != "admin" {
		t.Fatalf("unexpected user: %+v", u)
	}

	has, err = st.HasUsers()
	if err != nil || !has {
		t.Fatalf("expected HasUsers to be true after user creation")
	}

	// Duplicate user fails
	if _, err := st.CreateUser("admin", "anotherpass123", "admin"); err == nil {
		t.Fatalf("expected duplicate username error")
	}

	// Authenticate failure
	if _, err := st.AuthenticateUser("admin", "wrongpassword"); err == nil {
		t.Fatalf("expected auth error on wrong password")
	}
	if _, err := st.AuthenticateUser("unknown", "anypassword"); err == nil {
		t.Fatalf("expected auth error on unknown user")
	}

	// Authenticate success
	authU, err := st.AuthenticateUser("admin", "supersecret123")
	if err != nil {
		t.Fatalf("AuthenticateUser failed: %v", err)
	}
	if authU.ID != u.ID {
		t.Fatalf("user ID mismatch")
	}

	// Create Session
	sess, err := st.CreateSession(authU, 2*time.Hour)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if sess.Token == "" || sess.Username != "admin" {
		t.Fatalf("unexpected session: %+v", sess)
	}

	// Validate Session
	valSess, err := st.ValidateSession(sess.Token)
	if err != nil {
		t.Fatalf("ValidateSession failed: %v", err)
	}
	if valSess.Username != "admin" {
		t.Fatalf("validate session username mismatch: %+v", valSess)
	}

	// Change Password
	if err := st.ChangePassword("admin", "wrongpass", "brandnewpass123"); err == nil {
		t.Fatalf("expected error changing password with wrong old password")
	}
	if err := st.ChangePassword("admin", "supersecret123", "brandnewpass123"); err != nil {
		t.Fatalf("ChangePassword failed: %v", err)
	}

	// Old session should be revoked after password change
	if _, err := st.ValidateSession(sess.Token); err == nil {
		t.Fatalf("expected session to be revoked after password change")
	}

	// Login with new password
	newU, err := st.AuthenticateUser("admin", "brandnewpass123")
	if err != nil {
		t.Fatalf("AuthenticateUser with new password failed: %v", err)
	}

	// Create new session & delete
	sess2, err := st.CreateSession(newU, 1*time.Hour)
	if err != nil {
		t.Fatalf("CreateSession 2 failed: %v", err)
	}
	if err := st.DeleteSession(sess2.Token); err != nil {
		t.Fatalf("DeleteSession failed: %v", err)
	}
	if _, err := st.ValidateSession(sess2.Token); err == nil {
		t.Fatalf("expected session to be invalid after delete")
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
}
