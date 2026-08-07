//go:build windows

// Windows: zwei Quellen. Security-Event-Log 4800/4801 (braucht einmalig
// Audit-Policy + Leserecht, siehe README) funktioniert auch ohne laufenden
// Prozess; der In-Prozess-Watcher (StartWatcher, nur während serve) kommt
// ohne Setup aus. Beide werden gemergt, Duplikate entschärft der Sync.
package lockwatch

import (
	"bytes"
	"context"
	"os/exec"
	"sort"
	"time"

	"golang.org/x/sys/windows"

	"timetrack/internal/core/ports"
)

// Events wird als Service.LockEvents injiziert.
var Events = events

var rec recorder

func events(since time.Time) ([]ports.LockEvent, error) {
	evs := eventlog(since)
	evs = append(evs, rec.events()...)
	sort.Slice(evs, func(i, j int) bool { return evs[i].Time.Before(evs[j].Time) })
	return evs, nil
}

// eventlog fragt das Security-Log ab. Best effort: ohne Audit-Policy kommt
// nichts, ohne Leserecht ein Fehler — beides heißt schlicht "keine Events".
func eventlog(since time.Time) []ports.LockEvent {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	q := "*[System[(EventID=4800 or EventID=4801) and TimeCreated[@SystemTime>='" +
		since.UTC().Format(time.RFC3339) + "']]]"
	out, err := exec.CommandContext(ctx, "wevtutil", "qe", "Security", "/f:xml", "/q:"+q).Output()
	if err != nil {
		return nil
	}
	return parseWevtutil(bytes.NewReader(out))
}

// StartWatcher pollt alle 5s den Sperrzustand und meldet Übergänge; die
// Events landen im recorder und fließen über Events() in den Sync.
func StartWatcher(onTransition func()) {
	go func() {
		last := isLocked()
		for range time.Tick(5 * time.Second) {
			cur := isLocked()
			if cur == last {
				continue
			}
			last = cur
			rec.record(time.Now(), cur)
			onTransition()
		}
	}()
}

var (
	user32               = windows.NewLazySystemDLL("user32.dll")
	procOpenInputDesktop = user32.NewProc("OpenInputDesktop")
	procCloseDesktop     = user32.NewProc("CloseDesktop")
)

const desktopSwitchdesktop = 0x0100 // DESKTOP_SWITCHDESKTOP

// isLocked: bei gesperrtem Schirm ist der Secure Desktop (Winlogon) aktiv,
// den ein User-Prozess nicht öffnen darf → OpenInputDesktop schlägt fehl.
// ponytail: Heuristik (triggert auch beim UAC-Prompt) — für Pausenerkennung
// gut genug, echte Session-Notifications bräuchten eine Message-Pump.
func isLocked() bool {
	h, _, _ := procOpenInputDesktop.Call(0, 0, desktopSwitchdesktop)
	if h == 0 {
		return true
	}
	_, _, _ = procCloseDesktop.Call(h)
	return false
}
