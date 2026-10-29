//go:build !unix

package metrics

import "time"

func cpuTime() (time.Duration, bool) { return 0, false }
