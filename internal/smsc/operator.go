package smsc

import (
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dilshodgayibnazarov/smsc-sim/internal/config"
	"github.com/dilshodgayibnazarov/smsc-sim/internal/dlr"
	"github.com/dilshodgayibnazarov/smsc-sim/internal/metrics"
	"github.com/dilshodgayibnazarov/smsc-sim/internal/smpp"
	"github.com/dilshodgayibnazarov/smsc-sim/internal/throttle"
)

// Operator is the runtime form of one configured SMSC endpoint.
type Operator struct {
	name string
	cfgMu sync.RWMutex
	cfg config.Operator
	log *slog.Logger
	m   *metrics.Metrics
	srv *Server

	bucket  *throttle.Bucket // nil when no rate limit is configured
	limited bool
	dlr     *dlr.Engine

	allowTX, allowRX, allowTRX bool

	seedBase int64
	seqSeed  atomic.Int64
	msgSeq   atomic.Uint64
	sessSeq  atomic.Uint64

	mu        sync.Mutex
	sessions  map[*session]struct{}
	bindCount int
	boundAddr string // actual listen address, set once Start opens the listener

	concatMu sync.Mutex
	concatID map[string]concatEntry
}

type concatEntry struct {
	id   string
	when time.Time
}

func newOperator(cfg config.Operator, seedBase int64, log *slog.Logger, m *metrics.Metrics, srv *Server) *Operator {
	op := &Operator{
		name:     cfg.Name,
		cfg:      cfg,
		log:      log.With("operator", cfg.Name, "listen", cfg.Listen),
		m:        m,
		srv:      srv,
		dlr:      dlr.New(cfg.DLR),
		seedBase: seedBase,
		sessions: map[*session]struct{}{},
		concatID: map[string]concatEntry{},
	}
	if rate, burst, ok := cfg.Throttle.Rate(); ok {
		op.bucket = throttle.New(rate, burst)
		op.limited = true
	}
	// Pre-create the common metric series so dashboards show 0 instead of "no data".
	m.ActiveSessions.WithLabelValues(cfg.Name).Set(0)
	for _, r := range []string{"accepted", "throttled", "queue_full", "rejected"} {
		m.SubmitSM.WithLabelValues(cfg.Name, r)
	}
	for _, r := range []string{"ok", "auth_failed"} {
		for _, bt := range []string{"tx", "rx", "trx"} {
			m.Binds.WithLabelValues(cfg.Name, bt, r)
		}
	}

	if len(cfg.BindTypes) == 0 {
		op.allowTX, op.allowRX, op.allowTRX = true, true, true
	} else {
		for _, bt := range cfg.BindTypes {
			switch bt {
			case "tx":
				op.allowTX = true
			case "rx":
				op.allowRX = true
			case "trx":
				op.allowTRX = true
			}
		}
	}
	return op
}

func (op *Operator) setAddr(a string) {
	op.mu.Lock()
	op.boundAddr = a
	op.mu.Unlock()
}

// Name returns the operator's configured name.
func (op *Operator) Name() string { return op.name }

// Addr returns the address the operator's listener is actually bound to (useful
// when the config used port 0).
func (op *Operator) Addr() string {
	op.mu.Lock()
	defer op.mu.Unlock()
	return op.boundAddr
}

// newRNG returns a per-session RNG. With a fixed config seed the stream is
// reproducible across runs; sessions still get independent sub-streams.
func (op *Operator) newRNG() *rand.Rand {
	return rand.New(rand.NewSource(op.seedBase + op.seqSeed.Add(1)))
}

// authenticate matches a bind against the operator's accounts.
func (op *Operator) authenticate(b *smpp.Bind) (config.Account, smpp.Status) {
	for _, a := range op.cfg.Accounts {
		if a.SystemID != b.SystemID {
			continue
		}
		if a.Password != b.Password {
			return config.Account{}, smpp.ESME_RINVPASWD
		}
		if a.SystemType != "" && a.SystemType != b.SystemType {
			return config.Account{}, smpp.ESME_RINVSYSTYP
		}
		return a, smpp.ESME_ROK
	}
	return config.Account{}, smpp.ESME_RINVSYSID
}

func (op *Operator) bindAllowed(id smpp.CommandID) bool {
	switch id {
	case smpp.BindTransmitter:
		return op.allowTX
	case smpp.BindReceiver:
		return op.allowRX
	case smpp.BindTransceiver:
		return op.allowTRX
	default:
		return false
	}
}

// scVersion is the sc_interface_version the operator advertises in bind_resp.
func (op *Operator) scVersion() uint8 {
	switch op.cfg.SMPPVersion {
	case "3.3":
		return 0 // 3.3 has no sc_interface_version TLV
	case "5.0", "5":
		return smpp.Version50
	default:
		return smpp.Version34
	}
}

func (op *Operator) nextMessageID() string {
	return fmt.Sprintf("%08X", op.msgSeq.Add(1))
}

// messageIDFor returns the id to use in submit_sm_resp, reusing one id across
// the parts of a concatenated message when concat.shared_message_id is set.
func (op *Operator) messageIDFor(sm *smpp.SM) string {
	if !op.cfg.Concat.SharedMessageID {
		return op.nextMessageID()
	}
	ref, ok := concatRef(sm)
	if !ok {
		return op.nextMessageID()
	}
	key := sm.SourceAddr + "|" + sm.DestAddr + "|" + ref

	op.concatMu.Lock()
	defer op.concatMu.Unlock()
	now := time.Now()
	for k, e := range op.concatID {
		if now.Sub(e.when) > time.Minute {
			delete(op.concatID, k)
		}
	}
	if e, ok := op.concatID[key]; ok {
		return e.id
	}
	id := op.nextMessageID()
	op.concatID[key] = concatEntry{id: id, when: now}
	return id
}

// concatRef derives a stable reference for a multipart message from its
// sar_msg_ref_num TLV or, failing that, its UDH reference octet.
func concatRef(sm *smpp.SM) (string, bool) {
	if v, ok := sm.TLV(smpp.TagSARMsgRefNum); ok && len(v) > 0 {
		return "sar:" + strings.TrimRight(fmt.Sprintf("%x", v), ""), true
	}
	if sm.ESMClass&0x40 != 0 { // UDHI set
		msg := sm.ShortMessage
		if len(msg) >= 6 && msg[0] == 0x05 && msg[1] == 0x00 && msg[2] == 0x03 {
			return fmt.Sprintf("udh:%02x", msg[3]), true
		}
		if len(msg) >= 7 && msg[0] == 0x06 && msg[1] == 0x08 && msg[2] == 0x04 {
			return fmt.Sprintf("udh:%02x%02x", msg[3], msg[4]), true
		}
	}
	return "", false
}

func (op *Operator) addSession(s *session) {
	op.mu.Lock()
	op.sessions[s] = struct{}{}
	op.bindCount++
	op.mu.Unlock()
	op.m.ActiveSessions.WithLabelValues(op.cfg.Name).Inc()
}

func (op *Operator) removeSession(s *session) {
	op.mu.Lock()
	if _, ok := op.sessions[s]; ok {
		delete(op.sessions, s)
		op.bindCount--
		op.mu.Unlock()
		op.m.ActiveSessions.WithLabelValues(op.cfg.Name).Dec()
		return
	}
	op.mu.Unlock()
}

func (op *Operator) atBindLimit() bool {
	if op.cfg.MaxBinds <= 0 {
		return false
	}
	op.mu.Lock()
	defer op.mu.Unlock()
	return op.bindCount >= op.cfg.MaxBinds
}

// receiverSessions returns bound sessions able to receive deliver_sm (RX/TRX).
func (op *Operator) receiverSessions() []*session {
	op.mu.Lock()
	defer op.mu.Unlock()
	out := make([]*session, 0, len(op.sessions))
	for s := range op.sessions {
		if s.canReceive() {
			out = append(out, s)
		}
	}
	return out
}

// Sessions returns snapshots of all currently open sessions for this operator.
func (op *Operator) Sessions() []SessionSnapshot {
	op.mu.Lock()
	defer op.mu.Unlock()
	out := make([]SessionSnapshot, 0, len(op.sessions))
	for s := range op.sessions {
		out = append(out, s.describe())
	}
	return out
}

// UpdateConfig dynamically mutates the operator's runtime settings.
func (op *Operator) UpdateConfig(cfg config.Operator) {
	op.cfgMu.Lock()
	op.cfg = cfg
	op.cfgMu.Unlock()

	op.mu.Lock()
	defer op.mu.Unlock()
	if rate, burst, ok := cfg.Throttle.Rate(); ok {
		if op.bucket == nil {
			op.bucket = throttle.New(rate, burst)
		} else {
			op.bucket.SetRate(rate, burst)
		}
		op.limited = true
	} else {
		op.limited = false
	}

	op.dlr = dlr.New(cfg.DLR)

	op.allowTX, op.allowRX, op.allowTRX = false, false, false
	if len(cfg.BindTypes) == 0 {
		op.allowTX, op.allowRX, op.allowTRX = true, true, true
	} else {
		for _, bt := range cfg.BindTypes {
			switch bt {
			case "tx":
				op.allowTX = true
			case "rx":
				op.allowRX = true
			case "trx":
				op.allowTRX = true
			}
		}
	}
}

// Config returns a copy of the operator's configuration.
func (op *Operator) Config() config.Operator {
	op.cfgMu.RLock()
	defer op.cfgMu.RUnlock()
	return op.cfg
}

// Snapshot is a read-only view of an operator for the admin API.
type Snapshot struct {
	Name          string   `json:"name"`
	Listen        string   `json:"listen"`
	SMPPVersion   string   `json:"smpp_version"`
	Accounts      []string `json:"accounts"`
	BindTypes     []string `json:"bind_types"`
	MaxBinds      int      `json:"max_binds"`
	WindowSize    int      `json:"window_size"`
	RateLimited   bool     `json:"rate_limited"`
	ThrottleTPS   float64  `json:"throttle_tps,omitempty"`
	ThrottleBurst int      `json:"throttle_burst,omitempty"`
	ActiveBinds   int      `json:"active_binds"`
	DLREnabled    bool     `json:"dlr_enabled"`
	MessagesSeen  uint64   `json:"messages_seen"`
}

// Describe returns a read-only snapshot of the operator for the admin API.
func (op *Operator) Describe() Snapshot {
	op.mu.Lock()
	binds := op.bindCount
	op.mu.Unlock()
	accts := make([]string, len(op.cfg.Accounts))
	for i, a := range op.cfg.Accounts {
		accts[i] = a.SystemID
	}
	bt := op.cfg.BindTypes
	if len(bt) == 0 {
		bt = []string{"tx", "rx", "trx"}
	}
	tps, burst, _ := op.cfg.Throttle.Rate()
	return Snapshot{
		Name:          op.cfg.Name,
		Listen:        op.cfg.Listen,
		SMPPVersion:   op.cfg.SMPPVersion,
		Accounts:      accts,
		BindTypes:     bt,
		MaxBinds:      op.cfg.MaxBinds,
		WindowSize:    op.cfg.WindowSize,
		RateLimited:   op.limited,
		ThrottleTPS:   tps,
		ThrottleBurst: int(burst),
		ActiveBinds:   binds,
		DLREnabled:    op.dlr.Enabled(),
		MessagesSeen:  op.msgSeq.Load(),
	}
}
