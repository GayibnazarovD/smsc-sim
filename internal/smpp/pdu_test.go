package smpp

import (
	"bufio"
	"bytes"
	"testing"
)

func TestReadRawRoundTrip(t *testing.T) {
	body := EncodeBindResp("SIM-SMSC", Version34)
	frame := Marshal(BindTransceiverResp, ESME_ROK, 42, body)

	r := bufio.NewReader(bytes.NewReader(frame))
	p, err := ReadRaw(r)
	if err != nil {
		t.Fatalf("ReadRaw: %v", err)
	}
	if p.Header.ID != BindTransceiverResp {
		t.Errorf("ID = %s, want bind_transceiver_resp", p.Header.ID)
	}
	if p.Header.Status != ESME_ROK || p.Header.Seq != 42 {
		t.Errorf("header = %+v", p.Header)
	}
	if int(p.Header.Length) != len(frame) {
		t.Errorf("Length = %d, want %d", p.Header.Length, len(frame))
	}
}

func TestDecodeBind(t *testing.T) {
	w := &writer{}
	w.cstr("esme01")
	w.cstr("s3cr3t")
	w.cstr("SMPP")
	w.u8(Version34)
	w.u8(0)
	w.u8(0)
	w.cstr("")

	b, err := DecodeBind(w.bytesVal())
	if err != nil {
		t.Fatalf("DecodeBind: %v", err)
	}
	if b.SystemID != "esme01" || b.Password != "s3cr3t" || b.SystemType != "SMPP" {
		t.Fatalf("bind = %+v", b)
	}
	if b.InterfaceVersion != Version34 {
		t.Errorf("InterfaceVersion = %#x", b.InterfaceVersion)
	}
}

func TestDecodeSMWithTLV(t *testing.T) {
	in := &SM{
		SourceAddr:         "12345",
		DestAddr:           "998901234567",
		ESMClass:           0,
		RegisteredDelivery: 1,
		ShortMessage:       []byte("hello world"),
		TLVs:               []TLV{{Tag: TagUserMessageRef, Value: []byte{0x00, 0x2a}}},
	}
	out, err := DecodeSM(in.Encode())
	if err != nil {
		t.Fatalf("DecodeSM: %v", err)
	}
	if out.SourceAddr != in.SourceAddr || out.DestAddr != in.DestAddr {
		t.Fatalf("addr mismatch: %+v", out)
	}
	if string(out.ShortMessage) != "hello world" {
		t.Errorf("short_message = %q", out.ShortMessage)
	}
	if !out.WantsReceipt() {
		t.Errorf("WantsReceipt = false, want true")
	}
	v, ok := out.TLV(TagUserMessageRef)
	if !ok || !bytes.Equal(v, []byte{0x00, 0x2a}) {
		t.Errorf("TLV user_message_reference = %v, ok=%v", v, ok)
	}
}

func TestDecodeSMTruncated(t *testing.T) {
	if _, err := DecodeSM([]byte{0x00, 0x01}); err == nil {
		t.Fatal("expected error on truncated body")
	}
}

func TestSMPPv5AndStatusParsing(t *testing.T) {
	st, ok := ParseStatus("0x58")
	if !ok || st != ESME_RTHROTTLED {
		t.Fatalf("ParseStatus(0x58) = %v, %v; want ESME_RTHROTTLED", st, ok)
	}

	st59, ok := ParseStatus("ESME_RCONGESTION")
	if !ok || st59 != ESME_RCONGESTION {
		t.Fatalf("ParseStatus(ESME_RCONGESTION) = %v, %v; want ESME_RCONGESTION (0x59)", st59, ok)
	}

	if st59.String() != "ESME_RCONGESTION" {
		t.Errorf("String() = %q, want ESME_RCONGESTION", st59.String())
	}

	statuses := AllStatuses()
	if len(statuses) < 40 {
		t.Errorf("AllStatuses() count = %d, expected >= 40", len(statuses))
	}

	respBody := EncodeSubmitSMRespWithTLVs("", TLV{Tag: TagCongestionState, Value: []byte{90}})
	if len(respBody) < 6 {
		t.Fatalf("expected TLV bytes in response body, got %d bytes", len(respBody))
	}
}
