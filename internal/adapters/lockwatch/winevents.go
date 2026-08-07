// Package lockwatch liefert Bildschirm-Sperr-/Entsperr-Events des OS für die
// automatische Pausen-Rekonstruktion (Service.LockEvents).
package lockwatch

import (
	"encoding/xml"
	"io"
	"sync"
	"time"

	"timetrack/internal/core/ports"
)

// parseWevtutil liest die XML-Ausgabe von `wevtutil qe Security /f:xml`:
// eine Folge von <Event>-Elementen ohne Wurzelelement. 4800 = gesperrt,
// 4801 = entsperrt, alles andere wird ignoriert.
func parseWevtutil(r io.Reader) []ports.LockEvent {
	var evs []ports.LockEvent
	dec := xml.NewDecoder(r)
	for {
		tok, err := dec.Token()
		if err != nil {
			return evs
		}
		start, ok := tok.(xml.StartElement)
		if !ok || start.Name.Local != "Event" {
			continue
		}
		var ev struct {
			System struct {
				EventID     int `xml:"EventID"`
				TimeCreated struct {
					SystemTime string `xml:"SystemTime,attr"`
				} `xml:"TimeCreated"`
			} `xml:"System"`
		}
		if err := dec.DecodeElement(&ev, &start); err != nil {
			continue
		}
		if ev.System.EventID != 4800 && ev.System.EventID != 4801 {
			continue
		}
		ts, err := time.Parse(time.RFC3339Nano, ev.System.TimeCreated.SystemTime)
		if err != nil {
			continue
		}
		evs = append(evs, ports.LockEvent{Time: ts, Locked: ev.System.EventID == 4800})
	}
}

// recorder sammelt Events des In-Prozess-Watchers (Windows serve). Einträge
// älter als 24h fliegen beim Anhängen raus — Replays sind für den Sync
// idempotent, der Puffer muss nur die jüngste Vergangenheit abdecken.
type recorder struct {
	mu  sync.Mutex
	evs []ports.LockEvent
}

func (r *recorder) record(t time.Time, locked bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cutoff := t.Add(-24 * time.Hour)
	kept := r.evs[:0]
	for _, e := range r.evs {
		if e.Time.After(cutoff) {
			kept = append(kept, e)
		}
	}
	r.evs = append(kept, ports.LockEvent{Time: t, Locked: locked})
}

func (r *recorder) events() []ports.LockEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ports.LockEvent, len(r.evs))
	copy(out, r.evs)
	return out
}
