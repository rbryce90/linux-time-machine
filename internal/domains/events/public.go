// Package events is the "system events" domain: systemd journal entries
// captured from journalctl, stored locally, searchable via SQL and (later)
// semantic embeddings.
package events

import "time"

// Event is the public-facing shape of a stored journal entry. Other domains
// that want to correlate against events consume this type.
type Event struct {
	At       time.Time
	Priority int    // 0-7 syslog severity
	Unit     string // systemd unit (e.g. "sshd.service")
	Source   string // SYSLOG_IDENTIFIER or _COMM
	PID      int32
	Message  string
}

// API is what other domains may call. Kept minimal on purpose.
type API interface {
	EventsNear(at time.Time, window time.Duration, limit int) ([]Event, error)
}
