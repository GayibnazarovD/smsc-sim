package smsc

import "time"

// Event describes a notable runtime occurrence (bind, throttle, error, DLR, MO).
type Event struct {
	ID       string    `json:"id"`
	Time     time.Time `json:"time"`
	Operator string    `json:"operator"`
	Type     string    `json:"type"`    // "bind", "unbind", "submit_sm", "throttled", "queue_full", "dlr", "mo", "disconnect", "error"
	Level    string    `json:"level"`   // "info", "warn", "error", "success"
	Message  string    `json:"message"`
	Details  string    `json:"details,omitempty"`
}

// SessionSnapshot provides a read-only view of an active ESME connection.
type SessionSnapshot struct {
	ID          string    `json:"id"`
	Operator    string    `json:"operator"`
	RemoteAddr  string    `json:"remote_addr"`
	SystemID    string    `json:"system_id"`
	Mode        string    `json:"mode"`
	ConnectedAt time.Time `json:"connected_at"`
	LastRxAt    time.Time `json:"last_rx_at"`
	InFlight    int64     `json:"in_flight"`
}
