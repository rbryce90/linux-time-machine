package events

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Collector streams the systemd journal via `journalctl -f -o json` and
// batches entries into the store. Running journalctl as a subprocess keeps
// us off cgo / libsystemd bindings and works on any Linux with systemd.
type Collector struct {
	store       Store
	flushEvery  time.Duration
	maxBatch    int
	journalArgs []string
}

func NewCollector(store Store) *Collector {
	return &Collector{
		store:      store,
		flushEvery: 2 * time.Second,
		maxBatch:   200,
		journalArgs: []string{
			"-f",           // follow
			"-o", "json",   // structured output
			"--no-pager",
			"--since", "now",
		},
	}
}

func (c *Collector) Run(ctx context.Context) {
	for {
		if err := c.runOnce(ctx); err != nil && ctx.Err() == nil {
			log.Printf("events collector: %v; retrying in 5s", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}
		if ctx.Err() != nil {
			return
		}
	}
}

func (c *Collector) runOnce(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "journalctl", c.journalArgs...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	defer func() { _ = cmd.Wait() }()

	lines := make(chan []byte, 256)
	go readLines(stdout, lines)

	batch := make([]Event, 0, c.maxBatch)
	flush := time.NewTicker(c.flushEvery)
	defer flush.Stop()

	writeBatch := func() {
		if len(batch) == 0 {
			return
		}
		if err := c.store.WriteEvents(batch); err != nil {
			log.Printf("events collector: write batch: %v", err)
		}
		batch = batch[:0]
	}

	for {
		select {
		case <-ctx.Done():
			writeBatch()
			return nil
		case line, ok := <-lines:
			if !ok {
				writeBatch()
				return nil
			}
			evt, ok := parseLine(line)
			if !ok {
				continue
			}
			batch = append(batch, evt)
			if len(batch) >= c.maxBatch {
				writeBatch()
			}
		case <-flush.C:
			writeBatch()
		}
	}
}

func readLines(r io.Reader, out chan<- []byte) {
	defer close(out)
	scanner := bufio.NewScanner(r)
	// journal JSON lines are long — raise the buffer.
	buf := make([]byte, 0, 1<<16)
	scanner.Buffer(buf, 1<<20)
	for scanner.Scan() {
		b := make([]byte, len(scanner.Bytes()))
		copy(b, scanner.Bytes())
		out <- b
	}
}

// parseLine converts a journalctl JSON line into an Event. journalctl emits
// values that can be strings, numbers, or arrays of strings depending on the
// field and the underlying data; we handle the string/array cases which
// cover the fields we care about.
func parseLine(line []byte) (Event, bool) {
	var m map[string]any
	if err := json.Unmarshal(line, &m); err != nil {
		return Event{}, false
	}

	e := Event{Message: strOf(m["MESSAGE"])}
	if e.Message == "" {
		return Event{}, false
	}

	if pri := strOf(m["PRIORITY"]); pri != "" {
		if n, err := strconv.Atoi(pri); err == nil {
			e.Priority = n
		}
	}
	e.Unit = firstNonEmpty(strOf(m["_SYSTEMD_UNIT"]), strOf(m["UNIT"]))
	e.Source = firstNonEmpty(strOf(m["SYSLOG_IDENTIFIER"]), strOf(m["_COMM"]))

	pidStr := firstNonEmpty(strOf(m["_PID"]), strOf(m["SYSLOG_PID"]))
	if n, err := strconv.ParseInt(pidStr, 10, 32); err == nil {
		e.PID = int32(n)
	}

	// Prefer __REALTIME_TIMESTAMP (microseconds since unix epoch) when present;
	// fall back to now if absent.
	if tsStr := strOf(m["__REALTIME_TIMESTAMP"]); tsStr != "" {
		if us, err := strconv.ParseInt(tsStr, 10, 64); err == nil {
			e.At = time.Unix(0, us*int64(time.Microsecond))
			return e, true
		}
	}
	e.At = time.Now()
	return e, true
}

func strOf(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []any:
		if len(x) == 0 {
			return ""
		}
		return strOf(x[0])
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		return ""
	}
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
