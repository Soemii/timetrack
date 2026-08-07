//go:build !darwin && !windows

package lockwatch

import (
	"time"

	"timetrack/internal/core/ports"
)

// Events = nil: kein Lock-Tracking auf diesem OS.
var Events func(since time.Time) ([]ports.LockEvent, error)

func StartWatcher(onTransition func()) {}
