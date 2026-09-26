package smsc

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dilshodgayibnazarov/smsc-sim/internal/dlr"
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
	case smpp.DataSM:
		s.handleDataSM(raw)
	case smpp.SubmitMulti:
		s.handleSubmitMulti(raw)
	case smpp.QuerySM:
		s.handleQuerySM(raw)
	case smpp.CancelSM:
		s.handleCancelSM(raw)
	case smpp.ReplaceSM:
		s.handleReplaceSM(raw)
	case smpp.BroadcastSM:
		s.handleBroadcastSM(raw)
	case smpp.QueryBroadcastSM:
		s.handleQueryBroadcastSM(raw)
	case smpp.CancelBroadcastSM:
		s.handleCancelBroadcastSM(raw)
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

	// 1. Dynamic Error Simulation via ShortMessage Text keywords
	msgText := string(sm.Text())
	if handled, drop, nack, simStatus := checkSubmitSimulation(msgText); handled {
		if drop {
			s.op.m.SubmitSM.WithLabelValues(name, "dropped").Inc()
			s.op.srv.RecordEvent(name, "drop", "warn", "Simulated packet drop ([ERR_DROP])", fmt.Sprintf("From: %s To: %s", sm.SourceAddr, sm.DestAddr))
			return
		}
		if nack {
			s.op.m.SubmitSM.WithLabelValues(name, "rejected").Inc()
			s.op.srv.RecordEvent(name, "nack", "error", "Simulated GenericNACK ([ERR_NACK])", fmt.Sprintf("From: %s To: %s", sm.SourceAddr, sm.DestAddr))
			s.send(smpp.GenericNACK, smpp.ESME_RSYSERR, raw.Header.Seq, nil)
			return
		}
		s.op.m.SubmitSM.WithLabelValues(name, "rejected").Inc()
		s.op.srv.RecordEvent(name, "fault", "warn", fmt.Sprintf("Simulated status %s (0x%08X)", simStatus.String(), uint32(simStatus)), fmt.Sprintf("From: %s To: %s", sm.SourceAddr, sm.DestAddr))
		if simStatus == smpp.ESME_RCONGESTION {
			s.send(smpp.SubmitSMResp, simStatus, raw.Header.Seq, smpp.EncodeSubmitSMRespWithTLVs("", smpp.TLV{Tag: smpp.TagCongestionState, Value: []byte{95}}))
		} else {
			s.send(smpp.SubmitSMResp, simStatus, raw.Header.Seq, nil)
		}
		return
	}

	// 2. Configured Operator Fault Injection
	if s.op.cfg.Faults.SubmitErrorPct > 0 && pct(s.rng, s.op.cfg.Faults.SubmitErrorPct) {
		status := smpp.Status(s.op.cfg.Faults.SubmitStatus)
		if status == 0 {
			status = smpp.ESME_RSUBMITFAIL
		}
		s.op.m.SubmitSM.WithLabelValues(name, "rejected").Inc()
		s.op.srv.RecordEvent(name, "fault", "warn", fmt.Sprintf("Operator fault injection %s (0x%08X)", status.String(), uint32(status)), fmt.Sprintf("From: %s To: %s", sm.SourceAddr, sm.DestAddr))
		if status == smpp.ESME_RCONGESTION {
			s.send(smpp.SubmitSMResp, status, raw.Header.Seq, smpp.EncodeSubmitSMRespWithTLVs("", smpp.TLV{Tag: smpp.TagCongestionState, Value: []byte{90}}))
		} else {
			s.send(smpp.SubmitSMResp, status, raw.Header.Seq, nil)
		}
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
	msgText := string(sm.Text())
	drop, outcomeOverride, delayOverride := checkDLRSimulation(msgText)
	if drop {
		s.op.srv.RecordEvent(s.op.Name(), "dlr", "warn", fmt.Sprintf("DLR dropped via [DLR:DROP] (%s)", msgID), fmt.Sprintf("Dest: %s", sm.DestAddr))
		return
	}

	outcome := s.op.dlr.Pick(s.rng)
	if outcomeOverride != nil {
		outcome = *outcomeOverride
	}

	// SMPP Registered Delivery semantics:
	// 0x01: receipt requested on both success and failure (SMPP v3.4 / v5.0)
	// 0x02: receipt requested on delivery failure only (SMPP v3.4 / v5.0)
	// 0x03: receipt requested on successful delivery only (SMPP v5.0 new feature)
	reg := sm.RegisteredDelivery & 0x03
	if reg == 0x02 && outcome.Delivered() {
		return // Failure only, but outcome was delivered -> no DLR
	}
	if reg == 0x03 && !outcome.Delivered() {
		return // Success only (SMPP v5.0), but outcome was not delivered -> no DLR
	}

	delay := s.op.dlr.Delay(s.rng)
	if delayOverride != nil {
		delay = *delayOverride
	}

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
		s.op.srv.RecordEvent(s.op.Name(), "dlr", "info", fmt.Sprintf("DLR delivered: %s (%s, err=%d)", outcome.Stat, msgID, outcome.ErrCode), fmt.Sprintf("Dest: %s", sm.DestAddr))
	}()
}

// checkSubmitSimulation inspects the message text for error simulation tags.
func checkSubmitSimulation(text string) (handled bool, shouldDrop bool, nack bool, status smpp.Status) {
	upper := strings.ToUpper(text)
	if strings.Contains(upper, "[ERR_DROP]") {
		return true, true, false, 0
	}
	if strings.Contains(upper, "[ERR_NACK]") {
		return true, false, true, smpp.ESME_RSYSERR
	}
	if idx := strings.Index(upper, "[STATUS:"); idx != -1 {
		end := strings.Index(upper[idx:], "]")
		if end != -1 {
			val := strings.TrimSpace(text[idx+len("[STATUS:") : idx+end])
			if st, ok := smpp.ParseStatus(val); ok {
				return true, false, false, st
			}
		}
	}
	namedMap := map[string]smpp.Status{
		"[ERR_THROTTLED]":     smpp.ESME_RTHROTTLED,
		"[ERR_CONGESTION]":    smpp.ESME_RCONGESTION,
		"[ERR_MSGQFUL]":       smpp.ESME_RMSGQFUL,
		"[ERR_QUEUE_FULL]":    smpp.ESME_RMSGQFUL,
		"[ERR_INVDEST]":       smpp.ESME_RINVDSTADR,
		"[ERR_INVALID_DEST]":  smpp.ESME_RINVDSTADR,
		"[ERR_INVSRC]":        smpp.ESME_RINVSRCADR,
		"[ERR_INVALID_SRC]":   smpp.ESME_RINVSRCADR,
		"[ERR_SYSERR]":        smpp.ESME_RSYSERR,
		"[ERR_SYSTEM]":        smpp.ESME_RSYSERR,
		"[ERR_SUBMITFAIL]":    smpp.ESME_RSUBMITFAIL,
		"[ERR_PROHIBITED]":    smpp.ESME_RPROHIBITED,
		"[ERR_SERTYPUNAVAIL]": smpp.ESME_RSERTYPUNAVAIL,
		"[ERR_SERTYPDENIED]":  smpp.ESME_RSERTYPDENIED,
		"[ERR_SERTYPUNAUTH]":  smpp.ESME_RSERTYPUNAUTH,
		"[ERR_INVMSGLEN]":     smpp.ESME_RINVMSGLEN,
		"[ERR_INVESMCLASS]":   smpp.ESME_RINVESMCLASS,
		"[ERR_INVSRCTON]":     smpp.ESME_RINVSRCTON,
		"[ERR_INVDSTTON]":     smpp.ESME_RINVDSTTON,
	}
	for tag, st := range namedMap {
		if strings.Contains(upper, tag) {
			return true, false, false, st
		}
	}
	return false, false, false, 0
}

// checkDLRSimulation inspects the message text for DLR simulation overrides.
func checkDLRSimulation(text string) (drop bool, outcome *dlr.Outcome, delay *time.Duration) {
	upper := strings.ToUpper(text)
	if strings.Contains(upper, "[DLR:DROP]") {
		return true, nil, nil
	}

	if idx := strings.Index(upper, "[DLR_DELAY:"); idx != -1 {
		end := strings.Index(upper[idx:], "]")
		if end != -1 {
			raw := strings.TrimSpace(text[idx+len("[DLR_DELAY:") : idx+end])
			if d, err := time.ParseDuration(raw); err == nil {
				delay = &d
			}
		}
	}

	dlrKeywords := []struct {
		tag        string
		stat       string
		state      uint8
		defaultErr int
	}{
		{"[DLR:UNDELIV", "UNDELIV", smpp.StateUndeliverable, 1},
		{"[DLR:EXPIRED", "EXPIRED", smpp.StateExpired, 2},
		{"[DLR:REJECTD", "REJECTD", smpp.StateRejected, 4},
		{"[DLR:DELETVD", "DELETVD", smpp.StateDeleted, 3},
		{"[DLR:DELIVRD", "DELIVRD", smpp.StateDelivered, 0},
		{"[DLR:ACCEPTD", "ACCEPTD", smpp.StateAccepted, 0},
		{"[DLR:UNKNOWN", "UNKNOWN", smpp.StateUnknown, 0},
	}

	for _, k := range dlrKeywords {
		if idx := strings.Index(upper, k.tag); idx != -1 {
			end := strings.Index(upper[idx:], "]")
			if end != -1 {
				inner := text[idx+len(k.tag) : idx+end]
				errCode := k.defaultErr
				if strings.HasPrefix(inner, ":") {
					val := strings.TrimPrefix(inner, ":")
					if strings.HasPrefix(strings.ToLower(val), "0x") {
						if c, err := strconv.ParseInt(val[2:], 16, 32); err == nil {
							errCode = int(c)
						}
					} else {
						if c, err := strconv.Atoi(val); err == nil {
							errCode = c
						}
					}
				}
				outcome = &dlr.Outcome{
					Stat:    k.stat,
					State:   k.state,
					ErrCode: errCode,
				}
				break
			}
		}
	}

	return false, outcome, delay
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

func (s *session) handleDataSM(raw *smpp.RawPDU) {
	name := s.op.Name()
	if !s.canSubmit() {
		s.send(smpp.DataSMResp, smpp.ESME_RINVBNDSTS, raw.Header.Seq, nil)
		return
	}
	sm, err := smpp.DecodeSM(raw.Body)
	if err != nil {
		s.send(smpp.DataSMResp, smpp.ESME_RINVMSGLEN, raw.Header.Seq, nil)
		return
	}

	msgText := string(sm.Text())
	if handled, drop, nack, simStatus := checkSubmitSimulation(msgText); handled {
		if drop {
			return
		}
		if nack {
			s.send(smpp.GenericNACK, smpp.ESME_RSYSERR, raw.Header.Seq, nil)
			return
		}
		if simStatus == smpp.ESME_RCONGESTION {
			s.send(smpp.DataSMResp, simStatus, raw.Header.Seq, smpp.EncodeSubmitSMRespWithTLVs("", smpp.TLV{Tag: smpp.TagCongestionState, Value: []byte{95}}))
		} else {
			s.send(smpp.DataSMResp, simStatus, raw.Header.Seq, nil)
		}
		return
	}

	if s.op.limited && !s.op.bucket.Allow() {
		s.send(smpp.DataSMResp, smpp.ESME_RTHROTTLED, raw.Header.Seq, nil)
		return
	}

	msgID := s.op.messageIDFor(sm)
	acceptedAt := time.Now()
	s.send(smpp.DataSMResp, smpp.ESME_ROK, raw.Header.Seq, smpp.EncodeSubmitSMResp(msgID))
	s.op.srv.RecordEvent(name, "data_sm", "info", fmt.Sprintf("data_sm accepted (id=%s)", msgID), fmt.Sprintf("Dest: %s", sm.DestAddr))

	if sm.WantsReceipt() && s.op.dlr.Enabled() {
		s.scheduleReceipt(sm, msgID, acceptedAt)
	}
}

func (s *session) handleQuerySM(raw *smpp.RawPDU) {
	name := s.op.Name()
	msgID, _ := smpp.DecodeQuerySM(raw.Body)
	if msgID == "" {
		msgID = "00000001"
	}
	finalDate := time.Now().Format("0601021504")
	body := smpp.EncodeQuerySMResp(msgID, finalDate, smpp.StateDelivered, 0)
	s.send(smpp.QuerySMResp, smpp.ESME_ROK, raw.Header.Seq, body)
	s.op.srv.RecordEvent(name, "query_sm", "info", fmt.Sprintf("query_sm status DELIVRD for message %s", msgID), s.remoteAddr)
}

func (s *session) handleCancelSM(raw *smpp.RawPDU) {
	name := s.op.Name()
	s.send(smpp.CancelSMResp, smpp.ESME_ROK, raw.Header.Seq, nil)
	s.op.srv.RecordEvent(name, "cancel_sm", "info", "cancel_sm accepted", s.remoteAddr)
}

func (s *session) handleReplaceSM(raw *smpp.RawPDU) {
	name := s.op.Name()
	s.send(smpp.ReplaceSMResp, smpp.ESME_ROK, raw.Header.Seq, nil)
	s.op.srv.RecordEvent(name, "replace_sm", "info", "replace_sm accepted", s.remoteAddr)
}

func (s *session) handleSubmitMulti(raw *smpp.RawPDU) {
	name := s.op.Name()
	if !s.canSubmit() {
		s.send(smpp.SubmitMultiResp, smpp.ESME_RINVBNDSTS, raw.Header.Seq, nil)
		return
	}
	msgID := s.op.nextMessageID()
	body := smpp.EncodeSubmitMultiResp(msgID)
	s.send(smpp.SubmitMultiResp, smpp.ESME_ROK, raw.Header.Seq, body)
	s.op.srv.RecordEvent(name, "submit_multi", "info", fmt.Sprintf("submit_multi accepted (id=%s)", msgID), s.remoteAddr)
}

func (s *session) handleBroadcastSM(raw *smpp.RawPDU) {
	name := s.op.Name()
	msgID := s.op.nextMessageID()
	body := smpp.EncodeSubmitSMResp(msgID)
	s.send(smpp.BroadcastSMResp, smpp.ESME_ROK, raw.Header.Seq, body)
	s.op.srv.RecordEvent(name, "broadcast_sm", "info", fmt.Sprintf("broadcast_sm (Cell Broadcast) accepted (id=%s)", msgID), s.remoteAddr)
}

func (s *session) handleQueryBroadcastSM(raw *smpp.RawPDU) {
	name := s.op.Name()
	body := smpp.EncodeSubmitSMResp("00000001")
	s.send(smpp.QueryBroadcastSMResp, smpp.ESME_ROK, raw.Header.Seq, body)
	s.op.srv.RecordEvent(name, "query_broadcast_sm", "info", "query_broadcast_sm answered", s.remoteAddr)
}

func (s *session) handleCancelBroadcastSM(raw *smpp.RawPDU) {
	name := s.op.Name()
	s.send(smpp.CancelBroadcastSMResp, smpp.ESME_ROK, raw.Header.Seq, nil)
	s.op.srv.RecordEvent(name, "cancel_broadcast_sm", "info", "cancel_broadcast_sm accepted", s.remoteAddr)
}
