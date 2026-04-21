CREATE TABLE IF NOT EXISTS events (
  ts       INTEGER NOT NULL,     -- UnixNano
  priority INTEGER NOT NULL,     -- 0-7 syslog severity (0=emerg, 7=debug)
  unit     TEXT,                 -- systemd unit that emitted this entry
  source   TEXT,                 -- SYSLOG_IDENTIFIER or _COMM
  pid      INTEGER,
  message  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_events_ts ON events (ts);
CREATE INDEX IF NOT EXISTS idx_events_unit ON events (unit);
CREATE INDEX IF NOT EXISTS idx_events_priority ON events (priority);
