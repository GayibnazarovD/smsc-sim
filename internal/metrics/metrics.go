// Package metrics defines the Prometheus collectors exported by smsc-sim.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics bundles every collector so it can be passed explicitly instead of
// relying on global state (keeps tests isolated).
type Metrics struct {
	Binds          *prometheus.CounterVec
	ActiveSessions *prometheus.GaugeVec
	SubmitSM       *prometheus.CounterVec
	SubmitLatency  *prometheus.HistogramVec
	DLRSent        *prometheus.CounterVec
	MOSent         *prometheus.CounterVec
	PDURx          *prometheus.CounterVec
	PDUTx          *prometheus.CounterVec
}

// New registers all collectors on reg and returns the bundle.
func New(reg prometheus.Registerer) *Metrics {
	f := promauto.With(reg)
	return &Metrics{
		Binds: f.NewCounterVec(prometheus.CounterOpts{
			Name: "smscsim_binds_total",
			Help: "Bind requests received, by operator, bind type and result.",
		}, []string{"operator", "type", "result"}),
		ActiveSessions: f.NewGaugeVec(prometheus.GaugeOpts{
			Name: "smscsim_active_sessions",
			Help: "Currently bound sessions, by operator.",
		}, []string{"operator"}),
		SubmitSM: f.NewCounterVec(prometheus.CounterOpts{
			Name: "smscsim_submit_sm_total",
			Help: "submit_sm PDUs, by operator and result (accepted, throttled, queue_full, rejected).",
		}, []string{"operator", "result"}),
		SubmitLatency: f.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "smscsim_submit_resp_latency_seconds",
			Help:    "Simulated delay between submit_sm and submit_sm_resp.",
			Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5},
		}, []string{"operator"}),
		DLRSent: f.NewCounterVec(prometheus.CounterOpts{
			Name: "smscsim_dlr_total",
			Help: "Delivery receipts sent, by operator and stat word.",
		}, []string{"operator", "stat"}),
		MOSent: f.NewCounterVec(prometheus.CounterOpts{
			Name: "smscsim_mo_total",
			Help: "Mobile-originated deliver_sm messages sent, by operator.",
		}, []string{"operator"}),
		PDURx: f.NewCounterVec(prometheus.CounterOpts{
			Name: "smscsim_pdu_rx_total",
			Help: "PDUs received, by operator and command name.",
		}, []string{"operator", "command"}),
		PDUTx: f.NewCounterVec(prometheus.CounterOpts{
			Name: "smscsim_pdu_tx_total",
			Help: "PDUs sent, by operator and command name.",
		}, []string{"operator", "command"}),
	}
}
