package smsc

import (
	"bufio"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/dilshodgayibnazarov/smsc-sim/internal/smpp"
)

// ReceivedMOMessage records an incoming deliver_sm received by the virtual receiver.
type ReceivedMOMessage struct {
	ID         string    `json:"id"`
	Time       time.Time `json:"time"`
	Operator   string    `json:"operator"`
	Source     string    `json:"source"`
	SourceTON  uint8     `json:"source_ton"`
	SourceNPI  uint8     `json:"source_npi"`
	Dest       string    `json:"dest"`
	DestTON    uint8     `json:"dest_ton"`
	DestNPI    uint8     `json:"dest_npi"`
	Text       string    `json:"text"`
	DataCoding uint8     `json:"data_coding"`
	IsReceipt  bool      `json:"is_receipt"`
}

type virtualReceiverSession struct {
	operator string
	conn     net.Conn
	done     chan struct{}
	quit     chan struct{}
	mu       sync.Mutex
	closed   bool
}

func (vrs *virtualReceiverSession) close() {
	vrs.mu.Lock()
	defer vrs.mu.Unlock()
	if vrs.closed {
		return
	}
	vrs.closed = true
	close(vrs.quit)
	if vrs.conn != nil {
		_ = vrs.conn.Close()
	}
}

// StartVirtualReceiver binds a background test receiver session to the operator listener.
func (s *Server) StartVirtualReceiver(operator string) error {
	s.vrMu.Lock()
	defer s.vrMu.Unlock()

	if _, exists := s.virtualReceivers[operator]; exists {
		return nil // already running
	}

	op := s.OperatorByName(operator)
	if op == nil {
		return fmt.Errorf("unknown operator %q", operator)
	}

	addr := op.Addr()
	if addr == "" {
		return fmt.Errorf("operator %q is not listening", operator)
	}

	opCfg := op.Config()
	systemID := "virtual_rx"
	password := "virtual_pw"
	if len(opCfg.Accounts) > 0 {
		systemID = opCfg.Accounts[0].SystemID
		password = opCfg.Accounts[0].Password
	}

	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return fmt.Errorf("dial virtual receiver to %s (%s) failed: %w", operator, addr, err)
	}

	r := bufio.NewReader(conn)

	// Bind as Receiver (or Transceiver)
	bindCmd := smpp.BindReceiver
	if !op.allowRX && op.allowTRX {
		bindCmd = smpp.BindTransceiver
	}

	bindReq := &smpp.Bind{
		SystemID:         systemID,
		Password:         password,
		SystemType:       "virtual",
		InterfaceVersion: 0x34,
	}
	bindPDU := smpp.Marshal(bindCmd, smpp.ESME_ROK, 1, bindReq.Encode())
	_ = conn.SetDeadline(time.Now().Add(4 * time.Second))
	if _, err := conn.Write(bindPDU); err != nil {
		_ = conn.Close()
		return fmt.Errorf("write bind to %s failed: %w", operator, err)
	}

	bindResp, err := smpp.ReadRaw(r)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("read bind_resp from %s failed: %w", operator, err)
	}
	if bindResp.Header.Status != smpp.ESME_ROK {
		_ = conn.Close()
		return fmt.Errorf("bind rejected for virtual receiver on %s: %s",
			operator, bindResp.Header.Status.String())
	}

	// Clear connection deadline for persistent streaming
	_ = conn.SetDeadline(time.Time{})

	vrs := &virtualReceiverSession{
		operator: operator,
		conn:     conn,
		done:     make(chan struct{}),
		quit:     make(chan struct{}),
	}
	s.virtualReceivers[operator] = vrs

	s.RecordEvent(operator, "bind", "info",
		fmt.Sprintf("Virtual test receiver bound to %s (system_id=%s)", operator, systemID),
		"Ready to capture incoming deliver_sm MO messages")

	go s.runVirtualReceiver(vrs, r)

	return nil
}

func (s *Server) runVirtualReceiver(vrs *virtualReceiverSession, r *bufio.Reader) {
	defer func() {
		close(vrs.done)
		s.vrMu.Lock()
		if cur, ok := s.virtualReceivers[vrs.operator]; ok && cur == vrs {
			delete(s.virtualReceivers, vrs.operator)
		}
		s.vrMu.Unlock()
	}()

	for {
		pdu, err := smpp.ReadRaw(r)
		if err != nil {
			return
		}

		switch pdu.Header.ID {
		case smpp.EnquireLink:
			_ = vrs.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
			_, _ = vrs.conn.Write(smpp.Marshal(smpp.EnquireLinkResp, smpp.ESME_ROK, pdu.Header.Seq, nil))

		case smpp.DeliverSM:
			// Acknowledge deliver_sm immediately
			respBody := smpp.EncodeSubmitSMResp("")
			_ = vrs.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
			_, _ = vrs.conn.Write(smpp.Marshal(smpp.DeliverSMResp, smpp.ESME_ROK, pdu.Header.Seq, respBody))

			sm, decErr := smpp.DecodeSM(pdu.Body)
			if decErr == nil {
				isReceipt := sm.ESMClass&0x04 != 0
				msgText := string(sm.Text())

				s.vrMu.Lock()
				rec := ReceivedMOMessage{
					ID:         fmt.Sprintf("mo-%d", time.Now().UnixNano()),
					Time:       time.Now(),
					Operator:   vrs.operator,
					Source:     sm.SourceAddr,
					SourceTON:  sm.SourceAddrTON,
					SourceNPI:  sm.SourceAddrNPI,
					Dest:       sm.DestAddr,
					DestTON:    sm.DestAddrTON,
					DestNPI:    sm.DestAddrNPI,
					Text:       msgText,
					DataCoding: sm.DataCoding,
					IsReceipt:  isReceipt,
				}
				s.virtualMessages = append(s.virtualMessages, rec)
				if len(s.virtualMessages) > 100 {
					s.virtualMessages = s.virtualMessages[len(s.virtualMessages)-100:]
				}
				s.vrMu.Unlock()

				s.RecordEvent(vrs.operator, "mo", "success",
					fmt.Sprintf("Virtual receiver received MO deliver_sm: %s -> %s", sm.SourceAddr, sm.DestAddr),
					fmt.Sprintf("Text: %s | TON: %d NPI: %d", msgText, sm.SourceAddrTON, sm.SourceAddrNPI))
			}

		case smpp.Unbind:
			_ = vrs.conn.SetWriteDeadline(time.Now().Add(1 * time.Second))
			_, _ = vrs.conn.Write(smpp.Marshal(smpp.UnbindResp, smpp.ESME_ROK, pdu.Header.Seq, nil))
			return
		}
	}
}

// StopVirtualReceiver disconnects the virtual receiver session for the named operator.
func (s *Server) StopVirtualReceiver(operator string) error {
	s.vrMu.Lock()
	vrs, exists := s.virtualReceivers[operator]
	if !exists {
		s.vrMu.Unlock()
		return nil
	}
	delete(s.virtualReceivers, operator)
	s.vrMu.Unlock()

	vrs.close()
	<-vrs.done
	s.RecordEvent(operator, "unbind", "info", fmt.Sprintf("Virtual test receiver stopped for %s", operator), "")
	return nil
}

// IsVirtualReceiverActive checks whether a virtual test receiver is currently running for an operator.
func (s *Server) IsVirtualReceiverActive(operator string) bool {
	s.vrMu.RLock()
	defer s.vrMu.RUnlock()
	_, exists := s.virtualReceivers[operator]
	return exists
}

// VirtualReceiverMessages returns all captured MO messages, filtered by operator if not empty.
func (s *Server) VirtualReceiverMessages(operator string) []ReceivedMOMessage {
	s.vrMu.RLock()
	defer s.vrMu.RUnlock()

	var out []ReceivedMOMessage
	for i := len(s.virtualMessages) - 1; i >= 0; i-- {
		m := s.virtualMessages[i]
		if operator == "" || m.Operator == operator {
			out = append(out, m)
		}
	}
	return out
}

// ClearVirtualReceiverMessages flushes stored incoming messages.
func (s *Server) ClearVirtualReceiverMessages(operator string) {
	s.vrMu.Lock()
	defer s.vrMu.Unlock()

	if operator == "" {
		s.virtualMessages = nil
		return
	}
	filtered := make([]ReceivedMOMessage, 0, len(s.virtualMessages))
	for _, m := range s.virtualMessages {
		if m.Operator != operator {
			filtered = append(filtered, m)
		}
	}
	s.virtualMessages = filtered
}
