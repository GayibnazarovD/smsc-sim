// Package config loads and validates the smsc-sim YAML configuration and
// resolves per-operator settings against a shared `defaults` block.
package config

import (
	"fmt"
	"net"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// DefaultReceiptTemplate is the near-universal delivery-receipt short_message
// format. Placeholders are documented in docs/config-reference.md.
const DefaultReceiptTemplate = "id:{msgid} sub:001 dlvrd:{dlvrd} submit date:{submit} done date:{done} stat:{stat} err:{err} text:{text}"

// Config is the fully parsed configuration file.
type Config struct {
	// Seed seeds every RNG in the process. 0 means "derive from wall clock"
	// (non-reproducible); any non-zero value makes a run repeatable.
	Seed      int64      `yaml:"seed"`
	Log       Log        `yaml:"log"`
	Metrics   Listen     `yaml:"metrics"`
	Admin     Listen     `yaml:"admin"`
	Defaults  Operator   `yaml:"defaults"`
	Operators []Operator `yaml:"operators"`
}

// Log controls process logging.
type Log struct {
	Level  string `yaml:"level"`  // debug, info, warn, error
	Format string `yaml:"format"` // text, json
}

// Listen is an optional HTTP listener. Empty Addr disables it.
type Listen struct {
	Addr string `yaml:"listen"`
}

// Operator is one simulated SMSC endpoint. Fields also present in the top-level
// `defaults` block are inherited unless the operator overrides them.
type Operator struct {
	Name     string    `yaml:"name" json:"name"`
	Listen   string    `yaml:"listen" json:"listen"`
	Accounts []Account `yaml:"accounts" json:"accounts"`

	SMPPVersion         string   `yaml:"smpp_version" json:"smpp_version"` // "3.3" | "3.4" (default 3.4)
	BindTypes           []string `yaml:"bind_types" json:"bind_types"`   // tx, rx, trx; empty = all
	MaxBinds            int      `yaml:"max_binds" json:"max_binds"`    // 0 = unlimited
	WindowSize          int      `yaml:"window_size" json:"window_size"`  // in-flight submit_sm cap; 0 = unlimited
	EnquireLinkInterval Duration `yaml:"enquire_link_interval" json:"enquire_link_interval"`
	SessionIdleTimeout  Duration `yaml:"session_idle_timeout" json:"session_idle_timeout"`

	SubmitRespLatency Latency  `yaml:"submit_resp_latency" json:"submit_resp_latency"`
	Throttle          Throttle `yaml:"throttle" json:"throttle"`
	DLR               DLR      `yaml:"dlr" json:"dlr"`
	MO                MO       `yaml:"mo" json:"mo"`
	Faults            Faults   `yaml:"faults" json:"faults"`
	Concat            Concat   `yaml:"concat" json:"concat"`
	TLS               *TLS     `yaml:"tls,omitempty" json:"tls,omitempty"`
}

// Account is a valid bind credential set for an operator.
type Account struct {
	SystemID   string `yaml:"system_id" json:"system_id"`
	Password   string `yaml:"password" json:"password"`
	SystemType string `yaml:"system_type" json:"system_type"` // "" = accept any
}

// Latency describes the delay before a submit_sm_resp is sent.
type Latency struct {
	Dist   string   `yaml:"dist"` // fixed (default), uniform, exponential
	Mean   Duration `yaml:"mean"`
	Min    Duration `yaml:"min"`
	Max    Duration `yaml:"max"`
	Jitter Duration `yaml:"jitter"` // fixed dist only: +/- uniform jitter
}

// Throttle is a per-operator submit_sm rate limit. Exceeding it yields
// ESME_RTHROTTLED. Give either tps(+burst) or count+window.
type Throttle struct {
	TPS    float64  `yaml:"tps" json:"tps"`
	Burst  float64  `yaml:"burst" json:"burst"`
	Count  int      `yaml:"count" json:"count"`
	Window Duration `yaml:"window" json:"window"`
}

// Rate resolves the throttle to a token-bucket (rate tokens/sec, burst
// capacity). limited is false when no limit is configured.
func (t Throttle) Rate() (rate, burst float64, limited bool) {
	switch {
	case t.Count > 0 && t.Window.D() > 0:
		rate = float64(t.Count) / t.Window.D().Seconds()
		burst = float64(t.Count)
	case t.TPS > 0:
		rate = t.TPS
		burst = t.TPS
	default:
		return 0, 0, false
	}
	if t.Burst > 0 {
		burst = t.Burst
	}
	return rate, burst, true
}

// DLR configures the asynchronous delivery-receipt engine.
type DLR struct {
	Enabled  *bool          `yaml:"enabled" json:"enabled"` // nil => enabled
	Delay    Range          `yaml:"delay" json:"delay"`
	Outcomes map[string]int `yaml:"outcomes" json:"outcomes"`  // stat word -> weight
	ErrCodes map[string]int `yaml:"err_codes" json:"err_codes"` // stat word -> err value
	TLV      bool           `yaml:"tlv" json:"tlv"`       // append message_state / receipted_message_id / network_error_code
	Template string         `yaml:"receipt_template" json:"receipt_template"`
}

// IsEnabled reports whether receipts should be generated.
func (d DLR) IsEnabled() bool { return d.Enabled == nil || *d.Enabled }

// Range is an inclusive min/max duration window.
type Range struct {
	Min Duration `yaml:"min"`
	Max Duration `yaml:"max"`
}

// MO configures mobile-originated deliver_sm generation.
type MO struct {
	Enabled    bool    `yaml:"enabled"`
	RatePerMin float64 `yaml:"rate_per_min"`
	Source     string  `yaml:"source"`
	Text       string  `yaml:"text"`
}

// Faults injects protocol-level misbehaviour for resilience testing.
type Faults struct {
	RejectBindPct   float64  `yaml:"reject_bind_pct" json:"reject_bind_pct"`
	GenericNACKPct  float64  `yaml:"generic_nack_pct" json:"generic_nack_pct"`
	SubmitRejectPct float64  `yaml:"submit_reject_pct" json:"submit_reject_pct"`
	SubmitStatus    uint32   `yaml:"submit_status" json:"submit_status"`
	SubmitErrorPct  float64  `yaml:"submit_error_pct" json:"submit_error_pct"`
	DropAfter       Duration `yaml:"drop_after" json:"drop_after"` // close a bound session this long after bind; 0 = never
}

// Concat controls delivery-receipt message-id behaviour for multipart SMS.
type Concat struct {
	SharedMessageID bool `yaml:"shared_message_id"`
}

// TLS enables a TLS listener for an operator.
type TLS struct {
	Cert string `yaml:"cert"`
	Key  string `yaml:"key"`
}

var knownStats = map[string]bool{
	"DELIVRD": true, "EXPIRED": true, "DELETED": true, "UNDELIV": true,
	"ACCEPTD": true, "UNKNOWN": true, "REJECTD": true,
}

var knownBindTypes = map[string]bool{"tx": true, "rx": true, "trx": true}

// Load reads, parses and validates the config at path.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(raw)
}

// file mirrors Config but keeps operators as raw nodes so each can be decoded
// on top of a copy of Defaults (present keys override, absent keys inherit).
type file struct {
	Seed      int64       `yaml:"seed"`
	Log       Log         `yaml:"log"`
	Metrics   Listen      `yaml:"metrics"`
	Admin     Listen      `yaml:"admin"`
	Defaults  Operator    `yaml:"defaults"`
	Operators []yaml.Node `yaml:"operators"`
}

// Parse parses and validates config bytes.
func Parse(raw []byte) (*Config, error) {
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	dec.KnownFields(true)

	var f file
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	cfg := &Config{
		Seed:     f.Seed,
		Log:      f.Log,
		Metrics:  f.Metrics,
		Admin:    f.Admin,
		Defaults: f.Defaults,
	}

	for i, node := range f.Operators {
		op := cloneOperator(f.Defaults)
		if err := node.Decode(&op); err != nil {
			return nil, fmt.Errorf("operator[%d]: %w", i, err)
		}
		applyOperatorDefaults(&op)
		cfg.Operators = append(cfg.Operators, op)
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func cloneOperator(o Operator) Operator {
	c := o
	c.Accounts = append([]Account(nil), o.Accounts...)
	c.BindTypes = append([]string(nil), o.BindTypes...)
	c.DLR.Outcomes = cloneIntMap(o.DLR.Outcomes)
	c.DLR.ErrCodes = cloneIntMap(o.DLR.ErrCodes)
	if o.TLS != nil {
		t := *o.TLS
		c.TLS = &t
	}
	return c
}

func cloneIntMap(m map[string]int) map[string]int {
	if m == nil {
		return nil
	}
	c := make(map[string]int, len(m))
	for k, v := range m {
		c[k] = v
	}
	return c
}

func applyOperatorDefaults(op *Operator) {
	if op.SMPPVersion == "" {
		op.SMPPVersion = "3.4"
	}
	if op.DLR.Template == "" {
		op.DLR.Template = DefaultReceiptTemplate
	}
	if op.DLR.IsEnabled() && len(op.DLR.Outcomes) == 0 {
		op.DLR.Outcomes = map[string]int{"DELIVRD": 100}
	}
	if op.SubmitRespLatency.Dist == "" {
		op.SubmitRespLatency.Dist = "fixed"
	}
	for i := range op.BindTypes {
		op.BindTypes[i] = strings.ToLower(strings.TrimSpace(op.BindTypes[i]))
	}
	upperKeys(op.DLR.Outcomes)
	upperKeys(op.DLR.ErrCodes)
}

func upperKeys(m map[string]int) {
	if m == nil {
		return
	}
	for k, v := range m {
		u := strings.ToUpper(k)
		if u != k {
			delete(m, k)
			m[u] = v
		}
	}
}

func (c *Config) validate() error {
	if len(c.Operators) == 0 {
		return fmt.Errorf("config: no operators defined")
	}
	seenName := map[string]bool{}
	seenAddr := map[string]bool{}
	for _, op := range c.Operators {
		if op.Name == "" {
			return fmt.Errorf("config: operator with empty name")
		}
		if seenName[op.Name] {
			return fmt.Errorf("config: duplicate operator name %q", op.Name)
		}
		seenName[op.Name] = true

		if op.Listen == "" {
			return fmt.Errorf("operator %q: listen address is required", op.Name)
		}
		if _, _, err := net.SplitHostPort(op.Listen); err != nil {
			return fmt.Errorf("operator %q: invalid listen %q: %w", op.Name, op.Listen, err)
		}
		if seenAddr[op.Listen] {
			return fmt.Errorf("config: listen address %q used by more than one operator", op.Listen)
		}
		seenAddr[op.Listen] = true

		if len(op.Accounts) == 0 {
			return fmt.Errorf("operator %q: at least one account is required", op.Name)
		}
		for j, a := range op.Accounts {
			if a.SystemID == "" {
				return fmt.Errorf("operator %q: account[%d] has empty system_id", op.Name, j)
			}
		}
		if op.SMPPVersion != "3.3" && op.SMPPVersion != "3.4" && op.SMPPVersion != "5.0" && op.SMPPVersion != "5" {
			return fmt.Errorf("operator %q: smpp_version must be 3.3, 3.4, or 5.0, got %q", op.Name, op.SMPPVersion)
		}
		for _, bt := range op.BindTypes {
			if !knownBindTypes[bt] {
				return fmt.Errorf("operator %q: unknown bind type %q (want tx, rx or trx)", op.Name, bt)
			}
		}
		switch op.SubmitRespLatency.Dist {
		case "fixed", "uniform", "exponential":
		default:
			return fmt.Errorf("operator %q: submit_resp_latency.dist must be fixed, uniform or exponential", op.Name)
		}
		if op.Throttle.TPS < 0 || op.Throttle.Burst < 0 || op.Throttle.Count < 0 {
			return fmt.Errorf("operator %q: throttle values must not be negative", op.Name)
		}
		if op.DLR.IsEnabled() {
			total := 0
			for stat, w := range op.DLR.Outcomes {
				if !knownStats[stat] {
					return fmt.Errorf("operator %q: unknown dlr outcome %q", op.Name, stat)
				}
				if w < 0 {
					return fmt.Errorf("operator %q: dlr outcome %q weight is negative", op.Name, stat)
				}
				total += w
			}
			if total == 0 {
				return fmt.Errorf("operator %q: dlr.outcomes weights sum to zero", op.Name)
			}
			if d := op.DLR.Delay; d.Max.D() > 0 && d.Min.D() > d.Max.D() {
				return fmt.Errorf("operator %q: dlr.delay.min > dlr.delay.max", op.Name)
			}
		}
		if pct := []float64{op.Faults.RejectBindPct, op.Faults.GenericNACKPct, op.Faults.SubmitRejectPct}; true {
			for _, p := range pct {
				if p < 0 || p > 100 {
					return fmt.Errorf("operator %q: fault percentages must be within 0-100", op.Name)
				}
			}
		}
		if op.TLS != nil && (op.TLS.Cert == "" || op.TLS.Key == "") {
			return fmt.Errorf("operator %q: tls requires both cert and key", op.Name)
		}
	}
	return nil
}
