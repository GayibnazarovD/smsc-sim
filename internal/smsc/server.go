// Package smsc is the SMPP SMSC simulator core: one TCP listener per configured
// operator, each accepting binds and answering submit_sm with configurable
// latency, throttling, faults and asynchronous delivery receipts.
package smsc

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/dilshodgayibnazarov/smsc-sim/internal/config"
	"github.com/dilshodgayibnazarov/smsc-sim/internal/metrics"
	"github.com/dilshodgayibnazarov/smsc-sim/internal/smpp"
)

// Server owns every operator listener.
type Server struct {
	cfg       *config.Config
	startTime time.Time
	log       *slog.Logger
	m         *metrics.Metrics
	operators []*Operator
	listeners map[string]net.Listener

	mu        sync.Mutex
	sessions  map[*session]struct{}
	accepting bool

	eventsMu  sync.RWMutex
	events    []Event
	maxEvents int

	vrMu             sync.RWMutex
	virtualReceivers map[string]*virtualReceiverSession
	virtualMessages  []ReceivedMOMessage

	wg sync.WaitGroup
}

// New builds a Server from parsed config. Listeners are not opened until Start.
func New(cfg *config.Config, log *slog.Logger, m *metrics.Metrics) *Server {
	seed := cfg.Seed
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	srv := &Server{
		cfg:       cfg,
		startTime: time.Now(),
		log:       log,
		m:         m,
		listeners: map[string]net.Listener{},
		sessions:  map[*session]struct{}{},
		maxEvents: 250,
		virtualReceivers: make(map[string]*virtualReceiverSession),
	}
	// Give each operator a disjoint seed region so their RNG streams don't alias.
	base := rand.New(rand.NewSource(seed))
	for _, oc := range cfg.Operators {
		srv.operators = append(srv.operators, newOperator(oc, base.Int63(), log, m, srv))
	}
	return srv
}

// Operators exposes the operator list for the admin API.
func (s *Server) Operators() []*Operator {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Operator, len(s.operators))
	copy(out, s.operators)
	return out
}

// OperatorByName returns the named operator or nil if not found.
func (s *Server) OperatorByName(name string) *Operator {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, o := range s.operators {
		if o.cfg.Name == name {
			return o
		}
	}
	return nil
}

// Config returns the active simulator configuration.
func (s *Server) Config() *config.Config { return s.cfg }

// StartTime returns the timestamp when the server was created.
func (s *Server) StartTime() time.Time { return s.startTime }

// Uptime returns how long the server has been running.
func (s *Server) Uptime() time.Duration { return time.Since(s.startTime) }

// RecordEvent appends an event to the circular ring buffer. Safe for concurrent use.
func (s *Server) RecordEvent(operator, eventType, level, message, details string) {
	if s == nil {
		return
	}
	s.eventsMu.Lock()
	defer s.eventsMu.Unlock()

	ev := Event{
		ID:       fmt.Sprintf("ev-%d", time.Now().UnixNano()),
		Time:     time.Now(),
		Operator: operator,
		Type:     eventType,
		Level:    level,
		Message:  message,
		Details:  details,
	}
	s.events = append(s.events, ev)
	if len(s.events) > s.maxEvents {
		s.events = s.events[len(s.events)-s.maxEvents:]
	}
}

// Events returns the most recent events up to limit, newest first.
func (s *Server) Events(limit int) []Event {
	s.eventsMu.RLock()
	defer s.eventsMu.RUnlock()

	n := len(s.events)
	if limit <= 0 || limit > n {
		limit = n
	}
	out := make([]Event, limit)
	for i := 0; i < limit; i++ {
		out[i] = s.events[n-1-i]
	}
	return out
}

// Sessions returns snapshots of all currently open sessions across all operators.
func (s *Server) Sessions() []SessionSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]SessionSnapshot, 0, len(s.sessions))
	for sess := range s.sessions {
		out = append(out, sess.describe())
	}
	return out
}

// DisconnectSession closes an active session by its ID.
func (s *Server) DisconnectSession(id string) error {
	s.mu.Lock()
	var target *session
	for sess := range s.sessions {
		if sess.id == id {
			target = sess
			break
		}
	}
	s.mu.Unlock()

	if target == nil {
		return fmt.Errorf("session %q not found", id)
	}
	target.close()
	s.RecordEvent(target.op.cfg.Name, "disconnect", "warn", fmt.Sprintf("Admin disconnected session %s", id), target.remoteAddr)
	return nil
}

// Start opens a listener for every operator and begins accepting connections.
func (s *Server) Start() error {
	s.mu.Lock()
	s.accepting = true
	s.mu.Unlock()

	for _, op := range s.operators {
		if err := s.startOperatorListener(op); err != nil {
			_ = s.stopListeners()
			return err
		}
	}
	return nil
}

func (s *Server) startOperatorListener(op *Operator) error {
	ln, err := s.listen(op)
	if err != nil {
		return fmt.Errorf("operator %q: listen %s: %w", op.cfg.Name, op.cfg.Listen, err)
	}
	s.mu.Lock()
	s.listeners[op.cfg.Name] = ln
	s.mu.Unlock()

	op.setAddr(ln.Addr().String())
	s.wg.Add(1)
	go s.acceptLoop(op, ln)
	op.log.Info("listening", "addr", ln.Addr().String())
	s.RecordEvent(op.cfg.Name, "listen", "info", fmt.Sprintf("Listening on %s", ln.Addr().String()), op.cfg.SMPPVersion)
	return nil
}

// AddOperator dynamically registers and starts a new operator endpoint.
func (s *Server) AddOperator(oc config.Operator) error {
	s.mu.Lock()
	for _, o := range s.operators {
		if o.cfg.Name == oc.Name {
			s.mu.Unlock()
			return fmt.Errorf("operator %q already exists", oc.Name)
		}
	}
	op := newOperator(oc, time.Now().UnixNano(), s.log, s.m, s)
	s.operators = append(s.operators, op)
	accepting := s.accepting
	s.mu.Unlock()

	if accepting {
		if err := s.startOperatorListener(op); err != nil {
			// Rollback if listener failed to open
			s.mu.Lock()
			for i, o := range s.operators {
				if o == op {
					s.operators = append(s.operators[:i], s.operators[i+1:]...)
					break
				}
			}
			s.mu.Unlock()
			return err
		}
	}
	return nil
}

// UpdateOperator updates an existing operator's configuration and live listener.
func (s *Server) UpdateOperator(oldName string, oc config.Operator) error {
	s.mu.Lock()
	var target *Operator
	for _, o := range s.operators {
		if o.cfg.Name == oldName {
			target = o
			break
		}
	}
	if target == nil {
		s.mu.Unlock()
		return fmt.Errorf("operator %q not found", oldName)
	}

	listenChanged := target.cfg.Listen != oc.Listen
	nameChanged := oldName != oc.Name
	oldLn := s.listeners[oldName]
	s.mu.Unlock()

	if listenChanged && s.accepting {
		if oldLn != nil {
			_ = oldLn.Close()
			s.mu.Lock()
			delete(s.listeners, oldName)
			s.mu.Unlock()
		}
		target.UpdateConfig(oc)
		if err := s.startOperatorListener(target); err != nil {
			return fmt.Errorf("rebind %s: %w", oc.Listen, err)
		}
	} else {
		target.UpdateConfig(oc)
		if nameChanged {
			s.mu.Lock()
			if oldLn != nil {
				delete(s.listeners, oldName)
				s.listeners[oc.Name] = oldLn
			}
			s.mu.Unlock()
		}
	}

	s.RecordEvent(oc.Name, "config", "info", fmt.Sprintf("Operator %q configuration updated", oc.Name), oc.Listen)
	return nil
}

// DeleteOperator stops an operator's listener, disconnects sessions, and removes it.
func (s *Server) DeleteOperator(name string) error {
	s.mu.Lock()
	var target *Operator
	targetIdx := -1
	for i, o := range s.operators {
		if o.cfg.Name == name {
			target = o
			targetIdx = i
			break
		}
	}
	if target == nil {
		s.mu.Unlock()
		return fmt.Errorf("operator %q not found", name)
	}

	s.operators = append(s.operators[:targetIdx], s.operators[targetIdx+1:]...)
	ln, hasLn := s.listeners[name]
	if hasLn {
		delete(s.listeners, name)
	}

	// Close all sessions belonging to target
	for sess := range s.sessions {
		if sess.op == target {
			sess.close()
			delete(s.sessions, sess)
		}
	}
	s.mu.Unlock()

	if hasLn && ln != nil {
		_ = ln.Close()
	}

	s.RecordEvent(name, "delete", "warn", fmt.Sprintf("Operator %q deleted", name), "")
	return nil
}

func (s *Server) listen(op *Operator) (net.Listener, error) {
	ln, err := net.Listen("tcp", op.cfg.Listen)
	if err != nil {
		return nil, err
	}
	if op.cfg.TLS != nil {
		cert, err := tls.LoadX509KeyPair(op.cfg.TLS.Cert, op.cfg.TLS.Key)
		if err != nil {
			_ = ln.Close()
			return nil, fmt.Errorf("load tls keypair: %w", err)
		}
		ln = tls.NewListener(ln, &tls.Config{Certificates: []tls.Certificate{cert}})
	}
	return ln, nil
}

func (s *Server) acceptLoop(op *Operator, ln net.Listener) {
	defer s.wg.Done()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			op.log.Warn("accept error", "err", err)
			return
		}
		if tc, ok := conn.(*net.TCPConn); ok {
			_ = tc.SetKeepAlive(true)
			_ = tc.SetKeepAlivePeriod(30 * time.Second)
		}
		sess := newSession(op, conn)

		s.mu.Lock()
		if !s.accepting {
			s.mu.Unlock()
			_ = conn.Close()
			return
		}
		s.sessions[sess] = struct{}{}
		s.mu.Unlock()

		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			sess.serve()
			sess.wait()
			s.mu.Lock()
			delete(s.sessions, sess)
			s.mu.Unlock()
		}()
	}
}

// InjectMO sends a mobile-originated deliver_sm to one bound RX/TRX session of
// the named operator. It returns an error if the operator is unknown or has no
// eligible session.
func (s *Server) InjectMO(operator, source, dest, text string, dataCoding uint8) error {
	var op *Operator
	for _, o := range s.Operators() {
		if o.cfg.Name == operator {
			op = o
			break
		}
	}
	if op == nil {
		return fmt.Errorf("unknown operator %q", operator)
	}
	sessions := op.receiverSessions()
	if len(sessions) == 0 {
		return fmt.Errorf("operator %q has no bound receiver session", operator)
	}
	srcTON, srcNPI, cleanSrc := detectAddress(source)
	dstTON, dstNPI, cleanDst := detectAddress(dest)

	sm := &smpp.SM{
		SourceAddrTON: srcTON,
		SourceAddrNPI: srcNPI,
		SourceAddr:    cleanSrc,
		DestAddrTON:   dstTON,
		DestAddrNPI:   dstNPI,
		DestAddr:      cleanDst,
		DataCoding:    dataCoding,
		ShortMessage:  []byte(text),
	}
	target := sessions[op.newRNG().Intn(len(sessions))]
	err := target.sendMO(sm)
	if err == nil {
		s.RecordEvent(operator, "mo", "success", fmt.Sprintf("MO message injected to %s (TON=%d)", cleanDst, dstTON), fmt.Sprintf("Source: %s (TON=%d) | Text: %s", cleanSrc, srcTON, text))
	}
	return err
}

// detectAddress returns the SMPP Type-of-Number (TON) and Numbering-Plan-Identification (NPI)
// based on whether the address is an alphanumeric sender, short code, or international E.164 number.
func detectAddress(addr string) (ton, npi uint8, clean string) {
	addr = strings.TrimSpace(addr)
	hasLetter := false
	allDigits := true
	for _, ch := range addr {
		if unicode.IsLetter(ch) {
			hasLetter = true
			allDigits = false
		} else if !unicode.IsDigit(ch) && ch != '+' {
			allDigits = false
		}
	}
	clean = strings.TrimPrefix(addr, "+")

	switch {
	case hasLetter:
		// Alphanumeric Sender ID (e.g. TEXTUP, Google, Uber, BankAlert) -> TON 5, NPI 0
		return 5, 0, addr
	case len(clean) >= 3 && len(clean) <= 6 && allDigits:
		// Short Code (e.g. 3700, 1122, 999) -> TON 3, NPI 0
		return 3, 0, clean
	case len(clean) > 6 && allDigits:
		// International phone number E.164 (e.g. 998901234567, 14155552671) -> TON 1, NPI 1
		return 1, 1, clean
	default:
		return 0, 0, addr
	}
}

// Shutdown stops accepting, closes every session and waits for goroutines to
// drain or ctx to expire.
func (s *Server) Shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	s.accepting = false
	s.mu.Unlock()

	s.vrMu.Lock()
	for _, vrs := range s.virtualReceivers {
		vrs.close()
	}
	s.virtualReceivers = make(map[string]*virtualReceiverSession)
	s.vrMu.Unlock()

	_ = s.stopListeners()

	s.mu.Lock()
	for sess := range s.sessions {
		sess.close()
	}
	s.mu.Unlock()

	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Server) stopListeners() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var err error
	for name, ln := range s.listeners {
		if cerr := ln.Close(); cerr != nil && err == nil {
			err = cerr
		}
		delete(s.listeners, name)
	}
	return err
}
