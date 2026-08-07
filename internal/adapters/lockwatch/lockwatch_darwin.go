//go:build darwin

// macOS: loginwindow loggt com.apple.screenIsLocked/Unlocked ins Unified Log,
// abrufbar per /usr/bin/log show — kein Daemon, kein cgo nötig.
package lockwatch

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"strings"
	"time"

	"timetrack/internal/core/ports"
)

// Events wird als Service.LockEvents injiziert; nil auf nicht unterstützten OS.
var Events = events

// StartWatcher: no-op auf macOS — das Unified Log deckt alles rückwirkend ab.
func StartWatcher(onTransition func()) {}

const logPredicate = `process == "loginwindow" AND eventMessage CONTAINS "sendDistributedNotification: com.apple.screenIs"`

func events(since time.Time) ([]ports.LockEvent, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/log", "show",
		"--style", "ndjson",
		"--predicate", logPredicate,
		// log interpretiert --start in Lokalzeit.
		"--start", since.In(time.Local).Format("2006-01-02 15:04:05"))
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	evs := parse(out)
	if err := cmd.Wait(); err != nil {
		return nil, err
	}
	return evs, nil
}

// parse liest ndjson-Zeilen von log show; unpassende Zeilen (Trailer
// {"count":…,"finished":1}, kaputtes JSON, fremde Timestamps) werden übersprungen.
func parse(r io.Reader) []ports.LockEvent {
	var evs []ports.LockEvent
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var line struct {
			Timestamp    string `json:"timestamp"`
			EventMessage string `json:"eventMessage"`
		}
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
			continue
		}
		var locked bool
		switch {
		case strings.Contains(line.EventMessage, "screenIsLocked"):
			locked = true
		case strings.Contains(line.EventMessage, "screenIsUnlocked"):
			locked = false
		default:
			continue
		}
		ts, err := time.Parse("2006-01-02 15:04:05.000000-0700", line.Timestamp)
		if err != nil {
			continue
		}
		evs = append(evs, ports.LockEvent{Time: ts, Locked: locked})
	}
	return evs
}
