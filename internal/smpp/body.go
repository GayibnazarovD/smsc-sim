package smpp

// Bind is the decoded body of a bind_transmitter / bind_receiver /
// bind_transceiver PDU.
type Bind struct {
	SystemID         string
	Password         string
	SystemType       string
	InterfaceVersion uint8
	AddrTON          uint8
	AddrNPI          uint8
	AddressRange     string
}

// DecodeBind parses a bind request body.
func DecodeBind(body []byte) (*Bind, error) {
	r := &reader{b: body}
	b := &Bind{
		SystemID:         r.cstr(),
		Password:         r.cstr(),
		SystemType:       r.cstr(),
		InterfaceVersion: r.u8(),
		AddrTON:          r.u8(),
		AddrNPI:          r.u8(),
		AddressRange:     r.cstr(),
	}
	if r.err != nil {
		return nil, r.err
	}
	return b, nil
}

// Encode serialises a bind request body (system_id, password, system_type,
// interface_version, addr_ton, addr_npi, address_range).
func (b *Bind) Encode() []byte {
	w := &writer{}
	w.cstr(b.SystemID)
	w.cstr(b.Password)
	w.cstr(b.SystemType)
	w.u8(b.InterfaceVersion)
	w.u8(b.AddrTON)
	w.u8(b.AddrNPI)
	w.cstr(b.AddressRange)
	return w.bytesVal()
}

// BindResp is the decoded body of a bind response.
type BindResp struct {
	SystemID           string
	ScInterfaceVersion uint8
	TLVs               []TLV
}

// DecodeBindResp parses a bind response body.
func DecodeBindResp(body []byte) (*BindResp, error) {
	r := &reader{b: body}
	resp := &BindResp{
		SystemID: r.cstr(),
	}
	tlvs, err := decodeTLVs(r.rest())
	if err != nil {
		return nil, err
	}
	resp.TLVs = tlvs
	for _, t := range tlvs {
		if t.Tag == TagSCInterfaceVersion && len(t.Value) > 0 {
			resp.ScInterfaceVersion = t.Value[0]
		}
	}
	return resp, nil
}

// EncodeBindResp builds a bind_*_resp body: system_id C-Octet String plus an
// optional sc_interface_version TLV when scVersion != 0.
func EncodeBindResp(systemID string, scVersion uint8) []byte {
	w := &writer{}
	w.cstr(systemID)
	if scVersion != 0 {
		encodeTLVs(w, []TLV{{Tag: TagSCInterfaceVersion, Value: []byte{scVersion}}})
	}
	return w.bytesVal()
}

// SM is the decoded body shared by submit_sm and deliver_sm.
type SM struct {
	ServiceType          string
	SourceAddrTON        uint8
	SourceAddrNPI        uint8
	SourceAddr           string
	DestAddrTON          uint8
	DestAddrNPI          uint8
	DestAddr             string
	ESMClass             uint8
	ProtocolID           uint8
	PriorityFlag         uint8
	ScheduleDeliveryTime string
	ValidityPeriod       string
	RegisteredDelivery   uint8
	ReplaceIfPresent     uint8
	DataCoding           uint8
	SMDefaultMsgID       uint8
	ShortMessage         []byte
	TLVs                 []TLV
}

// DecodeSM parses a submit_sm / deliver_sm body including trailing TLVs.
func DecodeSM(body []byte) (*SM, error) {
	r := &reader{b: body}
	s := &SM{
		ServiceType:          r.cstr(),
		SourceAddrTON:        r.u8(),
		SourceAddrNPI:        r.u8(),
		SourceAddr:           r.cstr(),
		DestAddrTON:          r.u8(),
		DestAddrNPI:          r.u8(),
		DestAddr:             r.cstr(),
		ESMClass:             r.u8(),
		ProtocolID:           r.u8(),
		PriorityFlag:         r.u8(),
		ScheduleDeliveryTime: r.cstr(),
		ValidityPeriod:       r.cstr(),
		RegisteredDelivery:   r.u8(),
		ReplaceIfPresent:     r.u8(),
		DataCoding:           r.u8(),
		SMDefaultMsgID:       r.u8(),
	}
	smLen := int(r.u8())
	s.ShortMessage = append([]byte(nil), r.bytes(smLen)...)
	if r.err != nil {
		return nil, r.err
	}
	tlvs, err := decodeTLVs(r.rest())
	if err != nil {
		return nil, err
	}
	s.TLVs = tlvs
	return s, nil
}

// Encode serialises an SM body (used when the simulator sends deliver_sm).
func (s *SM) Encode() []byte {
	w := &writer{}
	w.cstr(s.ServiceType)
	w.u8(s.SourceAddrTON)
	w.u8(s.SourceAddrNPI)
	w.cstr(s.SourceAddr)
	w.u8(s.DestAddrTON)
	w.u8(s.DestAddrNPI)
	w.cstr(s.DestAddr)
	w.u8(s.ESMClass)
	w.u8(s.ProtocolID)
	w.u8(s.PriorityFlag)
	w.cstr(s.ScheduleDeliveryTime)
	w.cstr(s.ValidityPeriod)
	w.u8(s.RegisteredDelivery)
	w.u8(s.ReplaceIfPresent)
	w.u8(s.DataCoding)
	w.u8(s.SMDefaultMsgID)
	msg := s.ShortMessage
	if len(msg) > 254 {
		msg = msg[:254]
	}
	w.u8(uint8(len(msg)))
	w.raw(msg)
	encodeTLVs(w, s.TLVs)
	return w.bytesVal()
}

// TLV returns the value of the first optional parameter with tag.
func (s *SM) TLV(tag uint16) ([]byte, bool) { return tlvGet(s.TLVs, tag) }

// Text returns the message content, preferring the message_payload TLV when the
// mandatory short_message field is empty.
func (s *SM) Text() []byte {
	if len(s.ShortMessage) > 0 {
		return s.ShortMessage
	}
	if v, ok := s.TLV(TagMessagePayload); ok {
		return v
	}
	return nil
}

// WantsReceipt reports whether registered_delivery asks for an SMSC delivery
// receipt (bits 0-1 == 1 or 2 per section 5.2.17).
func (s *SM) WantsReceipt() bool {
	return s.RegisteredDelivery&0x03 != 0
}

// EncodeSubmitSMResp builds a submit_sm_resp body (message_id C-Octet String).
// For a non-zero command_status without TLVs the body is empty per spec.
func EncodeSubmitSMResp(messageID string) []byte {
	w := &writer{}
	w.cstr(messageID)
	return w.bytesVal()
}

// EncodeSubmitSMRespWithTLVs builds a submit_sm_resp body with optional TLVs
// (e.g. congestion_state for ESME_RCONGESTION in SMPP v5.0).
func EncodeSubmitSMRespWithTLVs(messageID string, tlvs ...TLV) []byte {
	w := &writer{}
	w.cstr(messageID)
	for _, t := range tlvs {
		w.u16(t.Tag)
		w.u16(uint16(len(t.Value)))
		w.raw(t.Value)
	}
	return w.bytesVal()
}

// DecodeMessageID reads a lone message_id C-Octet String body, used for
// submit_sm_resp and deliver_sm_resp.
func DecodeMessageID(body []byte) string {
	r := &reader{b: body}
	return r.cstr()
}
