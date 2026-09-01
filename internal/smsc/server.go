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
	"sync"
	"time"

	"github.com/dilshodgayibnazarov/smsc-sim/internal/config"
	"github.com/dilshodgayibnazarov/smsc-sim/internal/metrics"
	"github.com/dilshodgayibnazarov/smsc-sim/internal/smpp"
)

// Server owns every operator listener.
type Server struct {
	log       *slog.Logger
	m         *metrics.Metrics
	operators []*Operator
	listeners []net.Listener

	mu        sync.Mutex
	sessions  map[*session]struct{}
	accepting bool

	wg sync.WaitGroup
}

// New builds a Server from parsed config. Listeners are not opened until Start.
func New(cfg *config.Config, log *slog.Logger, m *metrics.Metrics) *Server {
	seed := cfg.Seed
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	srv := &Server{
		log:      log,
		m:        m,
		sessions: map[*session]struct{}{},
	}
	// Give each operator a disjoint seed region so their RNG streams don't alias.
	base := rand.New(rand.NewSource(seed))
	for _, oc := range cfg.Operators {
		srv.operators = append(srv.operators, newOperator(oc, base.Int63(), log, m))
	}
	return srv
}

// Operators exposes the operator list for the admin API.
func (s *Server) Operators() []*Operator { return s.operators }

// Start opens a listener for every operator and begins accepting connections.
func (s *Server) Start() error {
	s.mu.Lock()
	s.accepting = true
	s.mu.Unlock()

	for _, op := range s.operators {
		ln, err := s.listen(op)
		if err != nil {
			_ = s.stopListeners()
			return fmt.Errorf("operator %q: listen %s: %w", op.cfg.Name, op.cfg.Listen, err)
		}
		s.listeners = append(s.listeners, ln)
		op.setAddr(ln.Addr().String())
		s.wg.Add(1)
		go s.acceptLoop(op, ln)
		op.log.Info("listening", "addr", ln.Addr().String())
	}
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
	for _, o := range s.operators {
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
	sm := &smpp.SM{
		SourceAddr:   source,
		DestAddr:     dest,
		DataCoding:   dataCoding,
		ShortMessage: []byte(text),
	}
	target := sessions[op.newRNG().Intn(len(sessions))]
	return target.sendMO(sm)
}

// Shutdown stops accepting, closes every session and waits for goroutines to
// drain or ctx to expire.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	s.accepting = false
	s.mu.Unlock()

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
	var err error
	for _, ln := range s.listeners {
		if cerr := ln.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}
	s.listeners = nil
	return err
}
