// Package dlr builds asynchronous SMPP delivery receipts (deliver_sm) from a
// configurable outcome distribution.
package dlr

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"time"

	"github.com/dilshodgayibnazarov/smsc-sim/internal/config"
	"github.com/dilshodgayibnazarov/smsc-sim/internal/smpp"
)

// Outcome is a resolved delivery result.
type Outcome struct {
	Stat    string // DELIVRD, UNDELIV, EXPIRED, DELETED, ACCEPTD, UNKNOWN, REJECTD
	State   uint8  // SMPP message_state
	ErrCode int
}

// Delivered reports whether the outcome is a successful terminal delivery.
func (o Outcome) Delivered() bool { return o.Stat == "DELIVRD" }

var statToState = map[string]uint8{
	"DELIVRD": smpp.StateDelivered,
	"EXPIRED": smpp.StateExpired,
	"DELETED": smpp.StateDeleted,
	"UNDELIV": smpp.StateUndeliverable,
	"ACCEPTD": smpp.StateAccepted,
	"UNKNOWN": smpp.StateUnknown,
	"REJECTD": smpp.StateRejected,
}

var defaultErr = map[string]int{
	"DELIVRD": 0, "ACCEPTD": 0, "UNKNOWN": 0,
	"UNDELIV": 1, "EXPIRED": 2, "DELETED": 3, "REJECTD": 4,
}

// Engine produces receipts for one operator.
type Engine struct {
	cfg      config.DLR
	stats    []string
	cumulate []int
	total    int
}

// New builds an Engine from an operator's DLR config.
func New(cfg config.DLR) *Engine {
	e := &Engine{cfg: cfg}
	for stat := range cfg.Outcomes {
		e.stats = append(e.stats, stat)
	}
	sort.Strings(e.stats) // deterministic order for a given seed
	run := 0
	for _, stat := range e.stats {
		run += cfg.Outcomes[stat]
		e.cumulate = append(e.cumulate, run)
	}
	e.total = run
	return e
}

// Enabled reports whether receipts should be generated.
func (e *Engine) Enabled() bool { return e.cfg.IsEnabled() && e.total > 0 }

// Pick chooses an outcome according to the configured weights.
func (e *Engine) Pick(rng *rand.Rand) Outcome {
	stat := "DELIVRD"
	if e.total > 0 {
		n := rng.Intn(e.total)
		for i, c := range e.cumulate {
			if n < c {
				stat = e.stats[i]
				break
			}
		}
	}
	code, ok := e.cfg.ErrCodes[stat]
	if !ok {
		code = defaultErr[stat]
	}
	return Outcome{Stat: stat, State: statToState[stat], ErrCode: code}
}

// Delay returns how long to wait before sending the receipt.
func (e *Engine) Delay(rng *rand.Rand) time.Duration {
	min, max := e.cfg.Delay.Min.D(), e.cfg.Delay.Max.D()
	switch {
	case max <= 0 && min <= 0:
		return 0
	case max <= min:
		return min
	default:
		return min + time.Duration(rng.Int63n(int64(max-min)))
	}
}

// Build assembles the deliver_sm body for a receipt. orig is the received
// submit_sm, messageID the id returned in submit_sm_resp, submittedAt when the
// submit was accepted, and now the receipt time.
func (e *Engine) Build(orig *smpp.SM, messageID string, o Outcome, submittedAt, now time.Time) *smpp.SM {
	tmpl := e.cfg.Template
	if tmpl == "" {
		tmpl = config.DefaultReceiptTemplate
	}
	text := renderTemplate(tmpl, receiptFields{
		msgID:       messageID,
		stat:        o.Stat,
		errCode:     o.ErrCode,
		submittedAt: submittedAt,
		doneAt:      now,
		delivered:   o.Delivered(),
		origText:    string(orig.Text()),
		source:      orig.SourceAddr,
		dest:        orig.DestAddr,
	})

	sm := &smpp.SM{
		ServiceType:        orig.ServiceType,
		SourceAddrTON:      orig.DestAddrTON,
		SourceAddrNPI:      orig.DestAddrNPI,
		SourceAddr:         orig.DestAddr,
		DestAddrTON:        orig.SourceAddrTON,
		DestAddrNPI:        orig.SourceAddrNPI,
		DestAddr:           orig.SourceAddr,
		ESMClass:           smpp.ESMClassDeliveryReceipt,
		DataCoding:         smpp.DataCodingDefault,
		RegisteredDelivery: 0,
		ShortMessage:       []byte(text),
	}
	if e.cfg.TLV {
		sm.TLVs = append(sm.TLVs,
			smpp.TLV{Tag: smpp.TagReceiptedMessageID, Value: append([]byte(messageID), 0)},
			smpp.TLV{Tag: smpp.TagMessageStateOption, Value: []byte{o.State}},
			smpp.TLV{Tag: smpp.TagNetworkErrorCode, Value: []byte{0x03, byte(o.ErrCode >> 8), byte(o.ErrCode)}},
		)
	}
	return sm
}

type receiptFields struct {
	msgID       string
	stat        string
	errCode     int
	submittedAt time.Time
	doneAt      time.Time
	delivered   bool
	origText    string
	source      string
	dest        string
}

func renderTemplate(tmpl string, f receiptFields) string {
	dlvrd := "000"
	if f.delivered {
		dlvrd = "001"
	}
	text := f.origText
	if len(text) > 20 {
		text = text[:20]
	}
	r := strings.NewReplacer(
		"{msgid}", f.msgID,
		"{stat}", f.stat,
		"{err}", fmt.Sprintf("%03d", f.errCode),
		"{submit}", f.submittedAt.Format("0601021504"),
		"{done}", f.doneAt.Format("0601021504"),
		"{sub}", "001",
		"{dlvrd}", dlvrd,
		"{text}", text,
		"{source}", f.source,
		"{dest}", f.dest,
	)
	return r.Replace(tmpl)
}
