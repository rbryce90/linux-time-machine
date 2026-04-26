package types

import (
	"testing"
	"time"
)

func TestTimeRange_Duration(t *testing.T) {
	base := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name  string
		start time.Time
		end   time.Time
		want  time.Duration
	}{
		{"positive span", base, base.Add(5 * time.Minute), 5 * time.Minute},
		{"zero span", base, base, 0},
		{"negative span", base.Add(time.Hour), base, -time.Hour},
		{"sub-second", base, base.Add(250 * time.Millisecond), 250 * time.Millisecond},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := TimeRange{Start: c.start, End: c.end}
			if got := r.Duration(); got != c.want {
				t.Errorf("Duration() = %v, want %v", got, c.want)
			}
		})
	}
}
