CREATE TABLE IF NOT EXISTS system_samples (
  ts           INTEGER NOT NULL,
  cpu_pct      REAL    NOT NULL,
  mem_used     INTEGER NOT NULL,
  mem_total    INTEGER NOT NULL,
  disk_read    INTEGER NOT NULL,
  disk_write   INTEGER NOT NULL,
  net_rx       INTEGER NOT NULL,
  net_tx       INTEGER NOT NULL,
  PRIMARY KEY (ts)
);

CREATE INDEX IF NOT EXISTS idx_system_samples_ts
  ON system_samples (ts);

CREATE TABLE IF NOT EXISTS system_processes (
  ts      INTEGER NOT NULL,
  pid     INTEGER NOT NULL,
  name    TEXT    NOT NULL,
  cpu_pct REAL    NOT NULL,
  mem_rss INTEGER NOT NULL,
  PRIMARY KEY (ts, pid)
);

CREATE INDEX IF NOT EXISTS idx_system_processes_pid_ts
  ON system_processes (pid, ts);
