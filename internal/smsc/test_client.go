package smsc

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/dilshodgayibnazarov/smsc-sim/internal/smpp"
)

// SubmitTestRequest defines parameters for in-browser MT test submission.
type SubmitTestRequest struct {
	Operator           string `json:"operator"`
	SystemID           string `json:"system_id"`
	Password           string `json:"password"`
	SystemType         string `json:"system_type"`
	SMPPVersion        string `json:"smpp_version"` // "3.4" or "5.0"
	Command            string `json:"command"`      // "submit_sm" or "data_sm"
	Source             string `json:"source"`
	Dest               string `json:"dest"`
	Text               string `json:"text"`
	DataCoding         uint8  `json:"data_coding"`
	RegisteredDelivery uint8  `json:"registered_delivery"`
}

// TLVInfo contains decoded optional parameter details.
type TLVInfo struct {
	Tag      string `json:"tag"`
	Name     string `json:"name"`
	ValueHex string `json:"value_hex"`
}

// DLRInfo holds the delivery receipt result if captured synchronously.
type DLRInfo struct {
	Received          bool   `json:"received"`
	Scheduled         bool   `json:"scheduled"`
	ReceiptText       string `json:"receipt_text"`
	Status            string `json:"status"`
	ErrorCode         string `json:"error_code"`
	DeliveryLatencyMs int64  `json:"delivery_latency_ms"`
	Note              string `json:"note,omitempty"`
}

// SubmitTestResult represents the full protocol exchange report for the UI.
type SubmitTestResult struct {
	Operator          string    `json:"operator"`
	Command           string    `json:"command"`
	CommandStatus     string    `json:"command_status"`
	CommandStatusCode uint32    `json:"command_status_code"`
	CommandStatusHex  string    `json:"command_status_hex"`
	MessageID         string    `json:"message_id"`
	LatencyMs         int64     `json:"latency_ms"`
	SourceTON         uint8     `json:"source_ton"`
	SourceNPI         uint8     `json:"source_npi"`
	SourceAddr        string    `json:"source_addr"`
	DestTON           uint8     `json:"dest_ton"`
	DestNPI           uint8     `json:"dest_npi"`
	DestAddr          string    `json:"dest_addr"`
	TLVs              []TLVInfo `json:"tlvs,omitempty"`
	DLR               *DLRInfo  `json:"dlr,omitempty"`
	RawPDUHex         string    `json:"raw_pdu_hex,omitempty"`
}

// SubmitTest performs an end-to-end SMPP MT submission test against an operator listener.
func (s *Server) SubmitTest(req SubmitTestRequest) (*SubmitTestResult, error) {
	op := s.OperatorByName(req.Operator)
	if op == nil {
		return nil, fmt.Errorf("unknown operator %q", req.Operator)
	}

	addr := op.Addr()
	if addr == "" {
		return nil, fmt.Errorf("operator %q listener is not active", req.Operator)
	}

	// Resolve credentials: use first configured account if none supplied.
	systemID := req.SystemID
	password := req.Password
	opCfg := op.Config()
	if systemID == "" && len(opCfg.Accounts) > 0 {
		systemID = opCfg.Accounts[0].SystemID
		password = opCfg.Accounts[0].Password
	}
	if systemID == "" {
		systemID = "test"
		password = "test"
	}

	versionByte := uint8(0x34)
	if req.SMPPVersion == "5.0" || opCfg.SMPPVersion == "5.0" {
		versionByte = 0x50
	}

	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connect to %s (%s) failed: %w", req.Operator, addr, err)
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(6 * time.Second))
	r := bufio.NewReader(conn)

	// Step 1: Bind as Transceiver
	bindReq := &smpp.Bind{
		SystemID:         systemID,
		Password:         password,
		SystemType:       req.SystemType,
		InterfaceVersion: versionByte,
	}
	seq := uint32(1)
	bindPDU := smpp.Marshal(smpp.BindTransceiver, smpp.ESME_ROK, seq, bindReq.Encode())
	if _, err := conn.Write(bindPDU); err != nil {
		return nil, fmt.Errorf("write bind_transceiver failed: %w", err)
	}

	bindResp, err := smpp.ReadRaw(r)
	if err != nil {
		return nil, fmt.Errorf("read bind_transceiver_resp failed: %w", err)
	}
	if bindResp.Header.Status != smpp.ESME_ROK {
		return nil, fmt.Errorf("bind rejected by operator: %s (status 0x%08X)",
			bindResp.Header.Status.String(), uint32(bindResp.Header.Status))
	}

	// Step 2: Format source and destination addresses
	srcTON, srcNPI, cleanSrc := detectAddress(req.Source)
	dstTON, dstNPI, cleanDst := detectAddress(req.Dest)

	seq++
	cmdName := strings.ToLower(strings.TrimSpace(req.Command))
	var submitPDU []byte

	if cmdName == "data_sm" {
		cmdName = "data_sm"
		sm := &smpp.SM{
			SourceAddrTON:      srcTON,
			SourceAddrNPI:      srcNPI,
			SourceAddr:         cleanSrc,
			DestAddrTON:        dstTON,
			DestAddrNPI:        dstNPI,
			DestAddr:           cleanDst,
			RegisteredDelivery: req.RegisteredDelivery,
			DataCoding:         req.DataCoding,
			TLVs: []smpp.TLV{
				{Tag: smpp.TagMessagePayload, Value: []byte(req.Text)},
			},
		}
		submitPDU = smpp.Marshal(smpp.DataSM, smpp.ESME_ROK, seq, sm.Encode())
	} else {
		cmdName = "submit_sm"
		sm := &smpp.SM{
			SourceAddrTON:      srcTON,
			SourceAddrNPI:      srcNPI,
			SourceAddr:         cleanSrc,
			DestAddrTON:        dstTON,
			DestAddrNPI:        dstNPI,
			DestAddr:           cleanDst,
			RegisteredDelivery: req.RegisteredDelivery,
			DataCoding:         req.DataCoding,
			ShortMessage:       []byte(req.Text),
		}
		submitPDU = smpp.Marshal(smpp.SubmitSM, smpp.ESME_ROK, seq, sm.Encode())
	}

	// Step 3: Transmit PDU and measure latency
	start := time.Now()
	if _, err := conn.Write(submitPDU); err != nil {
		return nil, fmt.Errorf("write %s failed: %w", cmdName, err)
	}

	submitResp, err := smpp.ReadRaw(r)
	if err != nil {
		return nil, fmt.Errorf("read %s_resp failed: %w", cmdName, err)
	}
	latency := time.Since(start).Milliseconds()

	msgID := ""
	if len(submitResp.Body) > 0 {
		msgID = smpp.DecodeMessageID(submitResp.Body)
	}

	// Parse TLVs in response body
	var tlvInfos []TLVInfo
	if len(submitResp.Body) > 0 {
		nullIdx := -1
		for i, b := range submitResp.Body {
			if b == 0 {
				nullIdx = i
				break
			}
		}
		if nullIdx >= 0 && nullIdx+1 < len(submitResp.Body) {
			tlvs, _ := smpp.DecodeTLVs(submitResp.Body[nullIdx+1:])
			for _, t := range tlvs {
				name := fmt.Sprintf("0x%04x", t.Tag)
				if t.Tag == smpp.TagCongestionState {
					name = "congestion_state"
				} else if t.Tag == smpp.TagUserMessageRef {
					name = "user_message_ref"
				} else if t.Tag == smpp.TagNetworkErrorCode {
					name = "network_error_code"
				}
				tlvInfos = append(tlvInfos, TLVInfo{
					Tag:      fmt.Sprintf("0x%04X", t.Tag),
					Name:     name,
					ValueHex: hex.EncodeToString(t.Value),
				})
			}
		}
	}

	result := &SubmitTestResult{
		Operator:          req.Operator,
		Command:           cmdName,
		CommandStatus:     submitResp.Header.Status.String(),
		CommandStatusCode: uint32(submitResp.Header.Status),
		CommandStatusHex:  fmt.Sprintf("0x%08X", uint32(submitResp.Header.Status)),
		MessageID:         msgID,
		LatencyMs:         latency,
		SourceTON:         srcTON,
		SourceNPI:         srcNPI,
		SourceAddr:        cleanSrc,
		DestTON:           dstTON,
		DestNPI:           dstNPI,
		DestAddr:          cleanDst,
		TLVs:              tlvInfos,
		RawPDUHex:         hex.EncodeToString(submitResp.Body),
	}

	// Step 4: If registered delivery was requested and submission was accepted, poll for synchronous DLR
	if req.RegisteredDelivery != 0 && submitResp.Header.Status == smpp.ESME_ROK {
		result.DLR = &DLRInfo{
			Scheduled: true,
			Note:      "Delivery receipt scheduled asynchronously per carrier DLR profile.",
		}

		// Wait up to 1.2s to capture near-immediate delivery receipts
		_ = conn.SetDeadline(time.Now().Add(1200 * time.Millisecond))
		dlrStart := time.Now()
		for {
			dlrPDU, err := smpp.ReadRaw(r)
			if err != nil {
				break
			}
			if dlrPDU.Header.ID == smpp.DeliverSM {
				dlrSM, decErr := smpp.DecodeSM(dlrPDU.Body)
				// Acknowledge deliver_sm with deliver_sm_resp
				respBody := smpp.EncodeSubmitSMResp("")
				_ = conn.SetWriteDeadline(time.Now().Add(500 * time.Millisecond))
				_, _ = conn.Write(smpp.Marshal(smpp.DeliverSMResp, smpp.ESME_ROK, dlrPDU.Header.Seq, respBody))

				if decErr == nil {
					receiptText := string(dlrSM.Text())
					statusVal, errCode := extractDLRFields(receiptText)
					result.DLR = &DLRInfo{
						Received:          true,
						Scheduled:         true,
						ReceiptText:       receiptText,
						Status:            statusVal,
						ErrorCode:         errCode,
						DeliveryLatencyMs: time.Since(dlrStart).Milliseconds(),
					}
				}
				break
			} else if dlrPDU.Header.ID == smpp.EnquireLink {
				_ = conn.SetWriteDeadline(time.Now().Add(500 * time.Millisecond))
				_, _ = conn.Write(smpp.Marshal(smpp.EnquireLinkResp, smpp.ESME_ROK, dlrPDU.Header.Seq, nil))
			}
		}
	}

	// Step 5: Clean unbind
	seq++
	_ = conn.SetDeadline(time.Now().Add(500 * time.Millisecond))
	_, _ = conn.Write(smpp.Marshal(smpp.Unbind, smpp.ESME_ROK, seq, nil))
	_, _ = smpp.ReadRaw(r)

	return result, nil
}

// extractDLRFields parses stat: and err: values from SMPP delivery receipt text.
func extractDLRFields(receipt string) (stat, errCode string) {
	stat = "UNKNOWN"
	errCode = "000"

	parts := strings.Fields(receipt)
	for _, p := range parts {
		if strings.HasPrefix(strings.ToLower(p), "stat:") {
			stat = strings.ToUpper(strings.TrimPrefix(p, "stat:"))
		} else if strings.HasPrefix(strings.ToLower(p), "err:") {
			errCode = strings.TrimPrefix(p, "err:")
		}
	}
	return stat, errCode
}
