// Package store provides SQLite-backed persistent storage for simulator operator configurations,
// user authentication, and secure session management.
package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"

	"github.com/dilshodgayibnazarov/smsc-sim/internal/config"
)

var validUsernameRegex = regexp.MustCompile(`^[a-zA-Z0-9_\-\.\@]+$`)

// User represents an administrator or operator user in the system.
type User struct {
	ID        int64     `json:"id"`
	Username  string    `json:"username"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

// Session represents an active authenticated user session.
type Session struct {
	Token     string    `json:"token"`
	UserID    int64     `json:"user_id"`
	Username  string    `json:"username"`
	Role      string    `json:"role"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Store provides database persistence for operator connections and user authentication.
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
		fault_submit_status INTEGER NOT NULL DEFAULT 0,
		fault_submit_error_pct REAL NOT NULL DEFAULT 0,
		fault_drop_after TEXT NOT NULL DEFAULT '0s',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_operators_name ON operators(name);

	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE NOT NULL COLLATE NOCASE,
		password_hash TEXT NOT NULL,
		role TEXT NOT NULL DEFAULT 'admin',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);

	CREATE TABLE IF NOT EXISTS sessions (
		token TEXT PRIMARY KEY,
		user_id INTEGER NOT NULL,
		username TEXT NOT NULL,
		role TEXT NOT NULL,
		expires_at DATETIME NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
	);
	CREATE INDEX IF NOT EXISTS idx_sessions_token ON sessions(token);
	CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);
	`
	_, err := s.db.Exec(schema)
	if err != nil {
		return err
	}
	// Add newer columns for backward compatibility with existing databases
	_, _ = s.db.Exec(`ALTER TABLE operators ADD COLUMN fault_submit_status INTEGER NOT NULL DEFAULT 0`)
	_, _ = s.db.Exec(`ALTER TABLE operators ADD COLUMN fault_submit_error_pct REAL NOT NULL DEFAULT 0`)
	return nil
}

// HasUsers reports whether at least one user exists in the database.
func (s *Store) HasUsers() (bool, error) {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

// CreateUser hashes the password with bcrypt and stores a new user.
func (s *Store) CreateUser(username, password, role string) (*User, error) {
	username = strings.TrimSpace(username)
	if len(username) < 3 || len(username) > 64 {
		return nil, errors.New("username must be between 3 and 64 characters")
	}
	if !validUsernameRegex.MatchString(username) {
		return nil, errors.New("username can only contain letters, numbers, dots, underscores, dashes and @")
	}
	if len(password) < 8 {
		return nil, errors.New("password must be at least 8 characters long")
	}
	if role == "" {
		role = "admin"
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	res, err := s.db.Exec(
		`INSERT INTO users (username, password_hash, role) VALUES (?, ?, ?)`,
		username, string(hash), role,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, fmt.Errorf("user %q already exists", username)
		}
		return nil, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	return &User{
		ID:        id,
		Username:  username,
		Role:      role,
		CreatedAt: time.Now().UTC(),
	}, nil
}

// AuthenticateUser verifies user credentials and returns the matching user.
func (s *Store) AuthenticateUser(username, password string) (*User, error) {
	username = strings.TrimSpace(username)
	var (
		u    User
		hash string
	)
	err := s.db.QueryRow(
		`SELECT id, username, password_hash, role, created_at FROM users WHERE username = ?`,
		username,
	).Scan(&u.ID, &u.Username, &hash, &u.Role, &u.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("invalid username or password")
		}
		return nil, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return nil, errors.New("invalid username or password")
	}

	return &u, nil
}

// CreateSession generates a cryptographically secure random session token and stores it.
func (s *Store) CreateSession(user *User, ttl time.Duration) (*Session, error) {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}

	// Opportunistically prune expired sessions
	_, _ = s.db.Exec(`DELETE FROM sessions WHERE expires_at <= CURRENT_TIMESTAMP`)

	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("generate random token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)
	expiresAt := time.Now().UTC().Add(ttl)

	_, err := s.db.Exec(
		`INSERT INTO sessions (token, user_id, username, role, expires_at) VALUES (?, ?, ?, ?, ?)`,
		token, user.ID, user.Username, user.Role, expiresAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert session: %w", err)
	}

	return &Session{
		Token:     token,
		UserID:    user.ID,
		Username:  user.Username,
		Role:      user.Role,
		ExpiresAt: expiresAt,
	}, nil
}

// ValidateSession verifies if a session token is active and unexpired.
func (s *Store) ValidateSession(token string) (*Session, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, errors.New("missing session token")
	}

	var sess Session
	err := s.db.QueryRow(
		`SELECT token, user_id, username, role, expires_at FROM sessions WHERE token = ? AND expires_at > CURRENT_TIMESTAMP`,
		token,
	).Scan(&sess.Token, &sess.UserID, &sess.Username, &sess.Role, &sess.ExpiresAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("session invalid or expired")
		}
		return nil, err
	}

	return &sess, nil
}

// DeleteSession terminates a session.
func (s *Store) DeleteSession(token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	_, err := s.db.Exec(`DELETE FROM sessions WHERE token = ?`, token)
	return err
}

// ChangePassword updates a user's password after validating the old password,
// and revokes all active sessions for security.
func (s *Store) ChangePassword(username, oldPassword, newPassword string) error {
	u, err := s.AuthenticateUser(username, oldPassword)
	if err != nil {
		return err
	}

	if len(newPassword) < 8 {
		return errors.New("new password must be at least 8 characters long")
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash new password: %w", err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() // nolint:errcheck

	if _, err := tx.Exec(`UPDATE users SET password_hash = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, string(newHash), u.ID); err != nil {
		return err
	}

	// Revoke all sessions for this user
	if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id = ?`, u.ID); err != nil {
		return err
	}

	return tx.Commit()
}

// ClearOperators deletes all operators from the database.
func (s *Store) ClearOperators() error {
	_, err := s.db.Exec(`DELETE FROM operators`)
	return err
}

// ListOperators returns all operators stored in the database.
func (s *Store) ListOperators() ([]config.Operator, error) {
	rows, err := s.db.Query(`
		SELECT name, listen, smpp_version, accounts_json, bind_types_json,
		       max_binds, window_size, throttle_tps, throttle_burst,
		       latency_dist, latency_mean, latency_max, latency_jitter,
		       dlr_enabled, dlr_min_delay, dlr_max_delay,
		       fault_reject_bind_pct, fault_generic_nack_pct, fault_submit_reject_pct,
		       fault_submit_status, fault_submit_error_pct, fault_drop_after
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
		       fault_reject_bind_pct, fault_generic_nack_pct, fault_submit_reject_pct,
		       fault_submit_status, fault_submit_error_pct, fault_drop_after
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
			fault_reject_bind_pct, fault_generic_nack_pct, fault_submit_reject_pct,
			fault_submit_status, fault_submit_error_pct, fault_drop_after,
			updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`,
		op.Name, op.Listen, op.SMPPVersion, string(acctsJSON), string(bindTypesJSON),
		op.MaxBinds, op.WindowSize, tps, burst,
		op.SubmitRespLatency.Dist, op.SubmitRespLatency.Mean.String(), op.SubmitRespLatency.Max.String(), op.SubmitRespLatency.Jitter.String(),
		dlrEnabled, op.DLR.Delay.Min.String(), op.DLR.Delay.Max.String(),
		op.Faults.RejectBindPct, op.Faults.GenericNACKPct, op.Faults.SubmitRejectPct,
		op.Faults.SubmitStatus, op.Faults.SubmitErrorPct, op.Faults.DropAfter.String(),
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
			fault_reject_bind_pct = ?, fault_generic_nack_pct = ?, fault_submit_reject_pct = ?,
			fault_submit_status = ?, fault_submit_error_pct = ?, fault_drop_after = ?,
			updated_at = CURRENT_TIMESTAMP
		WHERE name = ?
	`,
		op.Name, op.Listen, op.SMPPVersion, string(acctsJSON), string(bindTypesJSON),
		op.MaxBinds, op.WindowSize, tps, burst,
		op.SubmitRespLatency.Dist, op.SubmitRespLatency.Mean.String(), op.SubmitRespLatency.Max.String(), op.SubmitRespLatency.Jitter.String(),
		dlrEnabled, op.DLR.Delay.Min.String(), op.DLR.Delay.Max.String(),
		op.Faults.RejectBindPct, op.Faults.GenericNACKPct, op.Faults.SubmitRejectPct,
		op.Faults.SubmitStatus, op.Faults.SubmitErrorPct, op.Faults.DropAfter.String(),
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
		submitStatus                                   uint32
		submitErrorPct                                 float64
		dropAfterStr                                   string
	)

	err := s.Scan(
		&op.Name, &op.Listen, &op.SMPPVersion, &accountsJSON, &bindTypesJSON,
		&op.MaxBinds, &op.WindowSize, &tps, &burst,
		&dist, &meanStr, &maxStr, &jitterStr,
		&dlrEnabledInt, &dlrMinStr, &dlrMaxStr,
		&rejectBindPct, &genericNACKPct, &submitRejectPct,
		&submitStatus, &submitErrorPct, &dropAfterStr,
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
	op.Faults.SubmitStatus = submitStatus
	op.Faults.SubmitErrorPct = submitErrorPct
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
