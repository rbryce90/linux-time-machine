// Package types holds small value types shared across domains.
// Nothing here depends on any domain; domains depend on this.
package types

import "time"

type ProcessID int32

type TimeRange struct {
	Start time.Time
	End   time.Time
}

func (r TimeRange) Duration() time.Duration {
	return r.End.Sub(r.Start)
}
