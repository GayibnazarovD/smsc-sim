// Package store provides SQLite-backed persistent storage for simulator operator configurations.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"github.com/dilshodgayibnazarov/smsc-sim/internal/config"
)

// Store provides database persistence for operator connections.
type Store struct {
	db *sql.DB
}

// Open initializes the SQLite database at dbPath and runs migrations.
// Pass ":memory:" for tests.
func Open(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", dbPath, err)
	}

	// Single connection / WAL-mode friendly settings for SQLite
	db.SetMaxOpenConns(1)

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// Close closes the underlying database handle.
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	const schema = `
	CREATE TABLE IF NOT EXISTS operators (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT UNIQUE NOT NULL,
		listen TEXT NOT NULL,
		smpp_version TEXT NOT NULL DEFAULT '3.4',
		accounts_json TEXT NOT NULL DEFAULT '[]',
		bind_types_json TEXT NOT NULL DEFAULT '["tx","rx","trx"]',
		max_binds INTEGER NOT NULL DEFAULT 0,
		window_size INTEGER NOT NULL DEFAULT 10,
		throttle_tps REAL NOT NULL DEFAULT 100,
		throttle_burst REAL NOT NULL DEFAULT 100,
		latency_dist TEXT NOT NULL DEFAULT 'fixed',
		latency_mean TEXT NOT NULL DEFAULT '30ms',
		latency_max TEXT NOT NULL DEFAULT '300ms',
		latency_jitter TEXT NOT NULL DEFAULT '10ms',
		dlr_enabled INTEGER NOT NULL DEFAULT 1,
		dlr_min_delay TEXT NOT NULL DEFAULT '3s',
		dlr_max_delay TEXT NOT NULL DEFAULT '25s',
		fault_reject_bind_pct REAL NOT NULL DEFAULT 0,
		fault_generic_nack_pct REAL NOT NULL DEFAULT 0,
		fault_submit_reject_pct REAL NOT NULL DEFAULT 0,
		fault_drop_after TEXT NOT NULL DEFAULT '0s',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_operators_name ON operators(name);
	`
	_, err := s.db.Exec(schema)
	return err
}

// ListOperators returns all operators stored in the database.
func (s *Store) ListOperators() ([]config.Operator, error) {
	rows, err := s.db.Query(`
		SELECT name, listen, smpp_version, accounts_json, bind_types_json,
		       max_binds, window_size, throttle_tps, throttle_burst,
		       latency_dist, latency_mean, latency_max, latency_jitter,
		       dlr_enabled, dlr_min_delay, dlr_max_delay,
		       fault_reject_bind_pct, fault_generic_nack_pct, fault_submit_reject_pct, fault_drop_after
		FROM operators ORDER BY id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []config.Operator
	for rows.Next() {
		op, err := scanOperator(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, op)
	}
	return list, rows.Err()
}

// GetOperator returns a single operator by name.
func (s *Store) GetOperator(name string) (*config.Operator, error) {
	row := s.db.QueryRow(`
		SELECT name, listen, smpp_version, accounts_json, bind_types_json,
		       max_binds, window_size, throttle_tps, throttle_burst,
		       latency_dist, latency_mean, latency_max, latency_jitter,
		       dlr_enabled, dlr_min_delay, dlr_max_delay,
		       fault_reject_bind_pct, fault_generic_nack_pct, fault_submit_reject_pct, fault_drop_after
		FROM operators WHERE name = ?
	`, name)

	op, err := scanOperator(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("operator %q not found", name)
		}
		return nil, err
	}
	return &op, nil
}

// CreateOperator inserts a new operator into the database.
func (s *Store) CreateOperator(op config.Operator) error {
	acctsJSON, _ := json.Marshal(op.Accounts)
	bindTypesJSON, _ := json.Marshal(op.BindTypes)
	tps, burst, _ := op.Throttle.Rate()

	dlrEnabled := 0
	if op.DLR.IsEnabled() {
		dlrEnabled = 1
	}

	_, err := s.db.Exec(`
		INSERT INTO operators (
			name, listen, smpp_version, accounts_json, bind_types_json,
			max_binds, window_size, throttle_tps, throttle_burst,
			latency_dist, latency_mean, latency_max, latency_jitter,
			dlr_enabled, dlr_min_delay, dlr_max_delay,
			fault_reject_bind_pct, fault_generic_nack_pct, fault_submit_reject_pct, fault_drop_after,
			updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`,
		op.Name, op.Listen, op.SMPPVersion, string(acctsJSON), string(bindTypesJSON),
		op.MaxBinds, op.WindowSize, tps, burst,
		op.SubmitRespLatency.Dist, op.SubmitRespLatency.Mean.String(), op.SubmitRespLatency.Max.String(), op.SubmitRespLatency.Jitter.String(),
		dlrEnabled, op.DLR.Delay.Min.String(), op.DLR.Delay.Max.String(),
		op.Faults.RejectBindPct, op.Faults.GenericNACKPct, op.Faults.SubmitRejectPct, op.Faults.DropAfter.String(),
	)
	return err
}

// UpdateOperator updates an existing operator by oldName.
func (s *Store) UpdateOperator(oldName string, op config.Operator) error {
	acctsJSON, _ := json.Marshal(op.Accounts)
	bindTypesJSON, _ := json.Marshal(op.BindTypes)
	tps, burst, _ := op.Throttle.Rate()

	dlrEnabled := 0
	if op.DLR.IsEnabled() {
		dlrEnabled = 1
	}

	res, err := s.db.Exec(`
		UPDATE operators SET
			name = ?, listen = ?, smpp_version = ?, accounts_json = ?, bind_types_json = ?,
			max_binds = ?, window_size = ?, throttle_tps = ?, throttle_burst = ?,
			latency_dist = ?, latency_mean = ?, latency_max = ?, latency_jitter = ?,
			dlr_enabled = ?, dlr_min_delay = ?, dlr_max_delay = ?,
			fault_reject_bind_pct = ?, fault_generic_nack_pct = ?, fault_submit_reject_pct = ?, fault_drop_after = ?,
			updated_at = CURRENT_TIMESTAMP
		WHERE name = ?
	`,
		op.Name, op.Listen, op.SMPPVersion, string(acctsJSON), string(bindTypesJSON),
		op.MaxBinds, op.WindowSize, tps, burst,
		op.SubmitRespLatency.Dist, op.SubmitRespLatency.Mean.String(), op.SubmitRespLatency.Max.String(), op.SubmitRespLatency.Jitter.String(),
		dlrEnabled, op.DLR.Delay.Min.String(), op.DLR.Delay.Max.String(),
		op.Faults.RejectBindPct, op.Faults.GenericNACKPct, op.Faults.SubmitRejectPct, op.Faults.DropAfter.String(),
		oldName,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("operator %q not found", oldName)
	}
	return nil
}

// DeleteOperator removes an operator from the database.
func (s *Store) DeleteOperator(name string) error {
	res, err := s.db.Exec(`DELETE FROM operators WHERE name = ?`, name)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("operator %q not found", name)
	}
	return nil
}

// SeedIfEmpty inserts the given list of operators if the operators table is currently empty.
func (s *Store) SeedIfEmpty(ops []config.Operator) error {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM operators`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	for _, op := range ops {
		if err := s.CreateOperator(op); err != nil {
			return fmt.Errorf("seed operator %q: %w", op.Name, err)
		}
	}
	return nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanOperator(s scanner) (config.Operator, error) {
	var (
		op                                             config.Operator
		accountsJSON, bindTypesJSON                    string
		tps, burst                                     float64
		dist, meanStr, maxStr, jitterStr               string
		dlrEnabledInt                                  int
		dlrMinStr, dlrMaxStr                           string
		rejectBindPct, genericNACKPct, submitRejectPct float64
		dropAfterStr                                   string
	)

	err := s.Scan(
		&op.Name, &op.Listen, &op.SMPPVersion, &accountsJSON, &bindTypesJSON,
		&op.MaxBinds, &op.WindowSize, &tps, &burst,
		&dist, &meanStr, &maxStr, &jitterStr,
		&dlrEnabledInt, &dlrMinStr, &dlrMaxStr,
		&rejectBindPct, &genericNACKPct, &submitRejectPct, &dropAfterStr,
	)
	if err != nil {
		return op, err
	}

	_ = json.Unmarshal([]byte(accountsJSON), &op.Accounts)
	_ = json.Unmarshal([]byte(bindTypesJSON), &op.BindTypes)

	if tps > 0 {
		op.Throttle.TPS = tps
		op.Throttle.Burst = burst
	}

	op.SubmitRespLatency.Dist = dist
	op.SubmitRespLatency.Mean = parseDuration(meanStr, 30*time.Millisecond)
	op.SubmitRespLatency.Max = parseDuration(maxStr, 300*time.Millisecond)
	op.SubmitRespLatency.Jitter = parseDuration(jitterStr, 10*time.Millisecond)

	enabled := dlrEnabledInt == 1
	op.DLR.Enabled = &enabled
	op.DLR.Delay.Min = parseDuration(dlrMinStr, 3*time.Second)
	op.DLR.Delay.Max = parseDuration(dlrMaxStr, 25*time.Second)
	op.DLR.Outcomes = map[string]int{
		"DELIVRD": 92,
		"UNDELIV": 5,
		"EXPIRED": 2,
		"REJECTD": 1,
	}

	op.Faults.RejectBindPct = rejectBindPct
	op.Faults.GenericNACKPct = genericNACKPct
	op.Faults.SubmitRejectPct = submitRejectPct
	op.Faults.DropAfter = parseDuration(dropAfterStr, 0)

	return op, nil
}

func parseDuration(s string, def time.Duration) config.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return config.Duration(def)
	}
	return config.Duration(d)
}
