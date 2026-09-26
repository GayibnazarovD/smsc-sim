package smsc

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dilshodgayibnazarov/smsc-sim/internal/smpp"
)

type bindMode int

const (
	modeUnbound bindMode = iota
	modeTX
	modeRX
	modeTRX
)

func (b bindMode) String() string {
	switch b {
	case modeTX:
		return "tx"
	case modeRX:
		return "rx"
	case modeTRX:
		return "trx"
	default:
		return "unbound"
	}
}

// session is one accepted TCP connection and its SMPP state.
type session struct {
	id          string
	connectedAt time.Time
	remoteAddr  string

	op   *Operator
	conn net.Conn
	br   *bufio.Reader
	log  *slog.Logger
	rng  *rand.Rand

	writeMu   sync.Mutex // serialises all writes to conn
	wg        sync.WaitGroup
	done      chan struct{}
	closeOnce sync.Once

	mode     atomic.Int32  // bindMode
	inFlight atomic.Int64  // submit_sm accepted but not yet responded
	seq      atomic.Uint32 // sequence numbers for server-originated PDUs
	lastRx   atomic.Int64  // unix nanos of last inbound PDU
	systemID string
}

func newSession(op *Operator, conn net.Conn) *session {
	s := &session{
		id:          fmt.Sprintf("sess-%s-%d", op.cfg.Name, op.sessSeq.Add(1)),
		connectedAt: time.Now(),
		remoteAddr:  conn.RemoteAddr().String(),
		op:          op,
		conn:        conn,
		br:          bufio.NewReaderSize(conn, 4096),
		log:         op.log.With("remote", conn.RemoteAddr().String()),
		rng:         op.newRNG(),
		done:        make(chan struct{}),
	}
	s.lastRx.Store(time.Now().UnixNano())
	return s
}

func (s *session) getMode() bindMode { return bindMode(s.mode.Load()) }
func (s *session) canReceive() bool {
	m := s.getMode()
	return m == modeRX || m == modeTRX
}
func (s *session) canSubmit() bool {
	m := s.getMode()
	return m == modeTX || m == modeTRX
}

func (s *session) nextSeq() uint32 { return s.seq.Add(1) }

func (s *session) describe() SessionSnapshot {
	lastRx := time.Unix(0, s.lastRx.Load())
	return SessionSnapshot{
		ID:          s.id,
		Operator:    s.op.Name(),
		RemoteAddr:  s.remoteAddr,
		SystemID:    s.systemID,
		Mode:        s.getMode().String(),
		ConnectedAt: s.connectedAt,
		LastRxAt:    lastRx,
		InFlight:    s.inFlight.Load(),
	}
}

// serve runs the read loop until the peer disconnects, an error occurs, or the
// server shuts the session down.
func (s *session) serve() {
	defer s.close()

	if iv := s.op.cfg.EnquireLinkInterval.D(); iv > 0 {
		s.wg.Add(1)
		go s.enquireLinkLoop(iv)
	}
	if it := s.op.cfg.SessionIdleTimeout.D(); it > 0 {
		s.wg.Add(1)
		go s.idleLoop(it)
	}
	if da := s.op.cfg.Faults.DropAfter.D(); da > 0 {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			select {
			case <-time.After(da):
				s.log.Info("fault: dropping session (drop_after)")
				s.close()
			case <-s.done:
			}
		}()
	}

	for {
		raw, err := smpp.ReadRaw(s.br)
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) && !isClosedConn(err) {
				s.log.Debug("read loop ended", "err", err)
			}
			return
		}
		s.lastRx.Store(time.Now().UnixNano())
		s.op.m.PDURx.WithLabelValues(s.op.Name(), raw.Header.ID.String()).Inc()
		if stop := s.dispatch(raw); stop {
			return
		}
	}
}

func (s *session) dispatch(raw *smpp.RawPDU) (stop bool) {
	switch raw.Header.ID {
	case smpp.BindTransmitter, smpp.BindReceiver, smpp.BindTransceiver:
		return s.handleBind(raw)
	case smpp.SubmitSM:
		s.handleSubmit(raw)
	case smpp.DeliverSMResp:
		// ESME acknowledged a receipt/MO we sent; nothing to do.
	case smpp.EnquireLink:
		s.send(smpp.EnquireLinkResp, smpp.ESME_ROK, raw.Header.Seq, nil)
	case smpp.EnquireLinkResp:
		// response to our heartbeat
	case smpp.Unbind:
		s.send(smpp.UnbindResp, smpp.ESME_ROK, raw.Header.Seq, nil)
		s.op.srv.RecordEvent(s.op.Name(), "unbind", "info", fmt.Sprintf("Account %q unbound", s.systemID), s.remoteAddr)
		return true
	case smpp.GenericNACK:
		// ignore
	default:
		s.send(smpp.GenericNACK, smpp.ESME_RINVCMDID, raw.Header.Seq, nil)
	}
	return false
}

func (s *session) handleBind(raw *smpp.RawPDU) (stop bool) {
	name := s.op.Name()
	btype := bindTypeLabel(raw.Header.ID)

	if s.getMode() != modeUnbound {
		s.op.m.Binds.WithLabelValues(name, btype, "already_bound").Inc()
		s.send(raw.Header.ID.RespID(), smpp.ESME_RALYBND, raw.Header.Seq, nil)
		return false
	}
	b, err := smpp.DecodeBind(raw.Body)
	if err != nil {
		s.op.m.Binds.WithLabelValues(name, btype, "malformed").Inc()
		s.send(raw.Header.ID.RespID(), smpp.ESME_RUNKNOWNERR, raw.Header.Seq, nil)
		return true
	}
	if !s.op.bindAllowed(raw.Header.ID) {
		s.op.m.Binds.WithLabelValues(name, btype, "bind_type_refused").Inc()
		s.op.srv.RecordEvent(name, "bind", "warn", fmt.Sprintf("Bind type refused (%s)", btype), s.remoteAddr)
		s.send(raw.Header.ID.RespID(), smpp.ESME_RBINDFAIL, raw.Header.Seq, nil)
		return true
	}
	if s.op.cfg.SMPPVersion == "3.4" && raw.Header.ID == smpp.BindTransceiver && b.InterfaceVersion != 0 && b.InterfaceVersion < smpp.Version34 {
		s.op.m.Binds.WithLabelValues(name, btype, "version_refused").Inc()
		s.op.srv.RecordEvent(name, "bind", "warn", fmt.Sprintf("Interface version refused (%s)", btype), s.remoteAddr)
		s.send(raw.Header.ID.RespID(), smpp.ESME_RBINDFAIL, raw.Header.Seq, nil)
		return true
	}
	if s.op.cfg.Faults.RejectBindPct > 0 && pct(s.rng, s.op.cfg.Faults.RejectBindPct) {
		s.op.m.Binds.WithLabelValues(name, btype, "fault_rejected").Inc()
		s.op.srv.RecordEvent(name, "bind", "warn", fmt.Sprintf("Bind fault rejected (%s)", btype), s.remoteAddr)
		s.send(raw.Header.ID.RespID(), smpp.ESME_RBINDFAIL, raw.Header.Seq, nil)
		return true
	}
	if _, st := s.op.authenticate(b); st != smpp.ESME_ROK {
		s.op.m.Binds.WithLabelValues(name, btype, "auth_failed").Inc()
		s.op.srv.RecordEvent(name, "bind", "error", fmt.Sprintf("Auth failed for system_id=%q", b.SystemID), s.remoteAddr)
		s.send(raw.Header.ID.RespID(), st, raw.Header.Seq, nil)
		return true
	}
	if s.op.atBindLimit() {
		s.op.m.Binds.WithLabelValues(name, btype, "bind_limit").Inc()
		s.op.srv.RecordEvent(name, "bind", "warn", fmt.Sprintf("Bind limit reached (%d binds max)", s.op.cfg.MaxBinds), s.remoteAddr)
		s.send(raw.Header.ID.RespID(), smpp.ESME_RBINDFAIL, raw.Header.Seq, nil)
		return true
	}

	switch raw.Header.ID {
	case smpp.BindTransmitter:
		s.mode.Store(int32(modeTX))
	case smpp.BindReceiver:
		s.mode.Store(int32(modeRX))
	case smpp.BindTransceiver:
		s.mode.Store(int32(modeTRX))
	}
	s.systemID = b.SystemID
	s.op.addSession(s)
	s.op.m.Binds.WithLabelValues(name, btype, "ok").Inc()
	s.op.srv.RecordEvent(name, "bind", "success", fmt.Sprintf("Account %q bound (%s mode)", b.SystemID, s.getMode().String()), s.remoteAddr)
	s.log.Info("bound", "mode", s.getMode().String(), "system_id", b.SystemID)
	s.send(raw.Header.ID.RespID(), smpp.ESME_ROK, raw.Header.Seq, smpp.EncodeBindResp("smsc-sim", s.op.scVersion()))
	return false
}

func (s *session) handleSubmit(raw *smpp.RawPDU) {
	name := s.op.Name()
	if !s.canSubmit() {
		s.op.m.SubmitSM.WithLabelValues(name, "rejected").Inc()
		s.send(smpp.SubmitSMResp, smpp.ESME_RINVBNDSTS, raw.Header.Seq, nil)
		return
	}
	sm, err := smpp.DecodeSM(raw.Body)
	if err != nil {
		s.op.m.SubmitSM.WithLabelValues(name, "rejected").Inc()
		s.send(smpp.SubmitSMResp, smpp.ESME_RINVMSGLEN, raw.Header.Seq, nil)
		return
	}
	if s.op.cfg.Faults.GenericNACKPct > 0 && pct(s.rng, s.op.cfg.Faults.GenericNACKPct) {
		s.op.m.SubmitSM.WithLabelValues(name, "rejected").Inc()
		s.send(smpp.GenericNACK, smpp.ESME_RSYSERR, raw.Header.Seq, nil)
		return
	}
	if s.op.cfg.Faults.SubmitRejectPct > 0 && pct(s.rng, s.op.cfg.Faults.SubmitRejectPct) {
		s.op.m.SubmitSM.WithLabelValues(name, "rejected").Inc()
		s.send(smpp.SubmitSMResp, smpp.ESME_RSUBMITFAIL, raw.Header.Seq, nil)
		return
	}
	if w := s.op.cfg.WindowSize; w > 0 && s.inFlight.Load() >= int64(w) {
		s.op.m.SubmitSM.WithLabelValues(name, "queue_full").Inc()
		s.op.srv.RecordEvent(name, "queue_full", "warn", fmt.Sprintf("Window queue full (%d inflight)", s.inFlight.Load()), s.remoteAddr)
		s.send(smpp.SubmitSMResp, smpp.ESME_RMSGQFUL, raw.Header.Seq, nil)
		return
	}
	if s.op.limited && !s.op.bucket.Allow() {
		s.op.m.SubmitSM.WithLabelValues(name, "throttled").Inc()
		s.op.srv.RecordEvent(name, "throttled", "warn", "submit_sm throttled (ESME_RTHROTTLED)", fmt.Sprintf("From: %s To: %s", sm.SourceAddr, sm.DestAddr))
		s.send(smpp.SubmitSMResp, smpp.ESME_RTHROTTLED, raw.Header.Seq, nil)
		return
	}

	msgID := s.op.messageIDFor(sm)
	acceptedAt := time.Now()
	latency := sampleLatency(s.op.cfg.SubmitRespLatency, s.rng)
	s.inFlight.Add(1)
	s.op.m.SubmitSM.WithLabelValues(name, "accepted").Inc()

	s.wg.Add(1)
	go func(seq uint32) {
		defer s.wg.Done()
		if latency > 0 {
			select {
			case <-time.After(latency):
			case <-s.done:
				s.inFlight.Add(-1)
				return
			}
		}
		s.op.m.SubmitLatency.WithLabelValues(name).Observe(time.Since(acceptedAt).Seconds())
		s.send(smpp.SubmitSMResp, smpp.ESME_ROK, seq, smpp.EncodeSubmitSMResp(msgID))
		s.inFlight.Add(-1)

		if sm.WantsReceipt() && s.op.dlr.Enabled() {
			s.scheduleReceipt(sm, msgID, acceptedAt)
		}
	}(raw.Header.Seq)
}

func (s *session) scheduleReceipt(sm *smpp.SM, msgID string, submittedAt time.Time) {
	outcome := s.op.dlr.Pick(s.rng)
	delay := s.op.dlr.Delay(s.rng)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-s.done:
				return
			}
		}
		body := s.op.dlr.Build(sm, msgID, outcome, submittedAt, time.Now()).Encode()
		if err := s.send(smpp.DeliverSM, smpp.ESME_ROK, s.nextSeq(), body); err != nil {
			return
		}
		s.op.m.DLRSent.WithLabelValues(s.op.Name(), outcome.Stat).Inc()
		s.op.srv.RecordEvent(s.op.Name(), "dlr", "info", fmt.Sprintf("DLR delivered: %s (%s)", outcome.Stat, msgID), fmt.Sprintf("Dest: %s", sm.DestAddr))
	}()
}

// sendMO delivers a mobile-originated message on this session.
func (s *session) sendMO(sm *smpp.SM) error {
	err := s.send(smpp.DeliverSM, smpp.ESME_ROK, s.nextSeq(), sm.Encode())
	if err == nil {
		s.op.m.MOSent.WithLabelValues(s.op.Name()).Inc()
	}
	return err
}

func (s *session) send(id smpp.CommandID, status smpp.Status, seq uint32, body []byte) error {
	frame := smpp.Marshal(id, status, seq, body)
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	select {
	case <-s.done:
		return net.ErrClosed
	default:
	}
	_ = s.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if _, err := s.conn.Write(frame); err != nil {
		s.log.Debug("write failed", "err", err, "command", id.String())
		return err
	}
	s.op.m.PDUTx.WithLabelValues(s.op.Name(), id.String()).Inc()
	return nil
}

func (s *session) enquireLinkLoop(interval time.Duration) {
	defer s.wg.Done()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-t.C:
			if s.send(smpp.EnquireLink, smpp.ESME_ROK, s.nextSeq(), nil) != nil {
				return
			}
		}
	}
}

func (s *session) idleLoop(timeout time.Duration) {
	defer s.wg.Done()
	t := time.NewTicker(timeout / 2)
	defer t.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-t.C:
			last := time.Unix(0, s.lastRx.Load())
			if time.Since(last) > timeout {
				s.log.Info("closing idle session", "idle", time.Since(last).Round(time.Second))
				s.close()
				return
			}
		}
	}
}

// close tears the session down. Safe to call repeatedly and from any goroutine.
func (s *session) close() {
	s.closeOnce.Do(func() {
		close(s.done)
		_ = s.conn.SetWriteDeadline(time.Now().Add(time.Second))
		_ = s.conn.Close()
		s.op.removeSession(s)
	})
}

// wait blocks until every background goroutine started by the session returns.
func (s *session) wait() { s.wg.Wait() }

func bindTypeLabel(id smpp.CommandID) string {
	switch id {
	case smpp.BindTransmitter:
		return "tx"
	case smpp.BindReceiver:
		return "rx"
	case smpp.BindTransceiver:
		return "trx"
	default:
		return "unknown"
	}
}

func isClosedConn(err error) bool {
	return err != nil && (errors.Is(err, net.ErrClosed) ||
		strings.Contains(err.Error(), "use of closed network connection"))
}
