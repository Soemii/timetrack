//go:build darwin

package lockwatch

import (
	"strings"
	"testing"
	"time"
)

const fixture = `{"timestamp":"2026-08-07 15:20:01.123456+0200","eventMessage":"-[SessionAgentNotificationCenter sendDistributedNotification:object:] | sendDistributedNotification: com.apple.screenIsLocked, with object:501"}
kaputte zeile ohne json
{"timestamp":"2026-08-07 15:27:44.208150+0200","eventMessage":"-[SessionAgentNotificationCenter sendDistributedNotification:object:] | sendDistributedNotification: com.apple.screenIsUnlocked, with object:501"}
{"timestamp":"nicht parsebar","eventMessage":"sendDistributedNotification: com.apple.screenIsLocked"}
{"count":2,"finished":1}
`

func TestParse(t *testing.T) {
	evs := parse(strings.NewReader(fixture))
	if len(evs) != 2 {
		t.Fatalf("erwartet 2 Events, got %d: %+v", len(evs), evs)
	}
	if !evs[0].Locked || evs[1].Locked {
		t.Errorf("Locked-Flags falsch: %+v", evs)
	}
	loc := time.FixedZone("", 2*3600)
	want := time.Date(2026, 8, 7, 15, 20, 1, 123456000, loc)
	if !evs[0].Time.Equal(want) {
		t.Errorf("Timestamp: %v, want %v", evs[0].Time, want)
	}
}
