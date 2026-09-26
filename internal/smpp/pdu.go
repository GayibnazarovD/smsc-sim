package smpp

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// HeaderLen is the fixed size of an SMPP PDU header in octets.
const HeaderLen = 16

// MaxPDULen caps an inbound PDU so a hostile or buggy peer cannot make the
// simulator allocate an arbitrary buffer. SMPP v3.4 messages are far smaller.
const MaxPDULen = 1 << 20 // 1 MiB

// ErrShortBuffer is returned when a body is truncated relative to its declared
// field lengths.
var ErrShortBuffer = errors.New("smpp: truncated PDU body")

// Header is the 16-octet SMPP PDU header.
type Header struct {
	Length uint32
	ID     CommandID
	Status Status
	Seq    uint32
}

// TLV is a single optional parameter (tag/length/value).
type TLV struct {
	Tag   uint16
	Value []byte
}

// RawPDU is a decoded header plus the still-encoded body+TLV bytes. Callers
// decode the body into a typed struct (Bind, SM) when they need its fields.
type RawPDU struct {
	Header Header
	Body   []byte
}

// ReadRaw reads one framed PDU from r. It returns io.EOF only when the reader
// is at a clean message boundary.
func ReadRaw(r *bufio.Reader) (*RawPDU, error) {
	var hdr [HeaderLen]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint32(hdr[0:4])
	if length < HeaderLen {
		return nil, fmt.Errorf("smpp: command_length %d below header size", length)
	}
	if length > MaxPDULen {
		return nil, fmt.Errorf("smpp: command_length %d exceeds max %d", length, MaxPDULen)
	}
	body := make([]byte, length-HeaderLen)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return &RawPDU{
		Header: Header{
			Length: length,
			ID:     CommandID(binary.BigEndian.Uint32(hdr[4:8])),
			Status: Status(binary.BigEndian.Uint32(hdr[8:12])),
			Seq:    binary.BigEndian.Uint32(hdr[12:16]),
		},
		Body: body,
	}, nil
}

// Marshal encodes a full PDU (header + body). command_length is computed.
func Marshal(id CommandID, status Status, seq uint32, body []byte) []byte {
	out := make([]byte, HeaderLen+len(body))
	binary.BigEndian.PutUint32(out[0:4], uint32(HeaderLen+len(body)))
	binary.BigEndian.PutUint32(out[4:8], uint32(id))
	binary.BigEndian.PutUint32(out[8:12], uint32(status))
	binary.BigEndian.PutUint32(out[12:16], seq)
	copy(out[HeaderLen:], body)
	return out
}

// --- low-level field helpers -------------------------------------------------

// reader walks a byte slice, tracking an offset and the first error hit.
type reader struct {
	b   []byte
	off int
	err error
}

func (r *reader) cstr() string {
	if r.err != nil {
		return ""
	}
	i := bytes.IndexByte(r.b[r.off:], 0)
	if i < 0 {
		r.err = ErrShortBuffer
		return ""
	}
	s := string(r.b[r.off : r.off+i])
	r.off += i + 1
	return s
}

func (r *reader) u8() uint8 {
	if r.err != nil {
		return 0
	}
	if r.off >= len(r.b) {
		r.err = ErrShortBuffer
		return 0
	}
	v := r.b[r.off]
	r.off++
	return v
}

func (r *reader) bytes(n int) []byte {
	if r.err != nil {
		return nil
	}
	if n < 0 || r.off+n > len(r.b) {
		r.err = ErrShortBuffer
		return nil
	}
	v := r.b[r.off : r.off+n]
	r.off += n
	return v
}

func (r *reader) rest() []byte {
	if r.err != nil {
		return nil
	}
	v := r.b[r.off:]
	r.off = len(r.b)
	return v
}

// writer accumulates encoded fields.
type writer struct{ buf bytes.Buffer }

func (w *writer) cstr(s string) { w.buf.WriteString(s); w.buf.WriteByte(0) }
func (w *writer) u8(v uint8)    { w.buf.WriteByte(v) }
func (w *writer) raw(b []byte)  { w.buf.Write(b) }
func (w *writer) u16(v uint16) {
	var b [2]byte
	binary.BigEndian.PutUint16(b[:], v)
	w.buf.Write(b[:])
}
func (w *writer) bytesVal() []byte { return w.buf.Bytes() }

// --- TLV -------------------------------------------------------------------

// DecodeTLVs parses optional parameters (tag/length/value) from byte slice.
func DecodeTLVs(b []byte) ([]TLV, error) {
	return decodeTLVs(b)
}

func decodeTLVs(b []byte) ([]TLV, error) {
	var out []TLV
	for len(b) > 0 {
		if len(b) < 4 {
			return out, ErrShortBuffer
		}
		tag := binary.BigEndian.Uint16(b[0:2])
		ln := int(binary.BigEndian.Uint16(b[2:4]))
		if 4+ln > len(b) {
			return out, ErrShortBuffer
		}
		val := make([]byte, ln)
		copy(val, b[4:4+ln])
		out = append(out, TLV{Tag: tag, Value: val})
		b = b[4+ln:]
	}
	return out, nil
}

func encodeTLVs(w *writer, tlvs []TLV) {
	for _, t := range tlvs {
		w.u16(t.Tag)
		w.u16(uint16(len(t.Value)))
		w.raw(t.Value)
	}
}

// Get returns the value of the first TLV with tag, and whether it was present.
func tlvGet(tlvs []TLV, tag uint16) ([]byte, bool) {
	for _, t := range tlvs {
		if t.Tag == tag {
			return t.Value, true
		}
	}
	return nil, false
}
