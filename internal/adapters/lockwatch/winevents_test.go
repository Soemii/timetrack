package lockwatch

import (
	"strings"
	"testing"
	"time"
)

const wevtFixture = `<Event xmlns='http://schemas.microsoft.com/win/2004/08/events/event'><System><Provider Name='Microsoft-Windows-Security-Auditing'/><EventID>4800</EventID><TimeCreated SystemTime='2026-08-07T13:20:01.1234567Z'/></System><EventData/></Event>
<Event xmlns='http://schemas.microsoft.com/win/2004/08/events/event'><System><EventID>4801</EventID><TimeCreated SystemTime='2026-08-07T13:27:44.0000000Z'/></System></Event>
<Event xmlns='http://schemas.microsoft.com/win/2004/08/events/event'><System><EventID>4624</EventID><TimeCreated SystemTime='2026-08-07T13:30:00.0000000Z'/></System></Event>`

func TestParseWevtutil(t *testing.T) {
	evs := parseWevtutil(strings.NewReader(wevtFixture))
	if len(evs) != 2 {
		t.Fatalf("erwartet 2 Events (4624 ignoriert), got %d: %+v", len(evs), evs)
	}
	if !evs[0].Locked || evs[1].Locked {
		t.Errorf("Locked-Flags falsch: %+v", evs)
	}
	want := time.Date(2026, 8, 7, 13, 20, 1, 123456700, time.UTC)
	if !evs[0].Time.Equal(want) {
		t.Errorf("Timestamp: %v, want %v", evs[0].Time, want)
	}
}

func TestRecorderPrunes24h(t *testing.T) {
	var r recorder
	t0 := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	r.record(t0, true)
	r.record(t0.Add(30*time.Hour), false) // verdrängt t0 (älter als 24h)
	evs := r.events()
	if len(evs) != 1 || evs[0].Locked {
		t.Errorf("Pruning: %+v", evs)
	}
}

func TestRecorderKeepsOrder(t *testing.T) {
	var r recorder
	t0 := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	r.record(t0, true)
	r.record(t0.Add(time.Minute), false)
	evs := r.events()
	if len(evs) != 2 || !evs[0].Locked || evs[1].Locked {
		t.Errorf("Reihenfolge: %+v", evs)
	}
}
