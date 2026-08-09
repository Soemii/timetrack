package service

import (
	"slices"
	"sort"
	"strconv"
	"testing"
	"time"

	"timetrack/internal/core/domain"
	"timetrack/internal/core/ports"
)

// lockService: testService + injizierte Lock-Event-Quelle (Slice per Pointer
// austauschbar, Counter zählt Aufrufe).
func lockService(t *testing.T, events *[]ports.LockEvent, calls *int) (*Service, *time.Time) {
	t.Helper()
	svc, now := testService(t)
	svc.LockEvents = func(since time.Time) ([]ports.LockEvent, error) {
		*calls++
		return slices.Clone(*events), nil
	}
	return svc, now
}

func segments(t *testing.T, svc *Service) []domain.Segment {
	t.Helper()
	f := svc.repo.(*fakeRepo)
	var out []domain.Segment
	for _, e := range f.entries {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out
}

func at(base time.Time, d time.Duration) time.Time { return base.Add(d) }

func TestLockSyncPauseAndResume(t *testing.T) {
	var events []ports.LockEvent
	var calls int
	svc, now := lockService(t, &events, &calls)
	start := *now
	if err := svc.Start("acme"); err != nil {
		t.Fatal(err)
	}
	events = []ports.LockEvent{
		{Time: at(start, 30*time.Minute), Locked: true},
		{Time: at(start, 50*time.Minute), Locked: false},
	}
	*now = at(start, 2*time.Hour)
	st, err := svc.Status()
	if err != nil {
		t.Fatal(err)
	}
	if st.State != StateWorking || !st.Since.Equal(at(start, 50*time.Minute)) {
		t.Errorf("Status nach Sync: %+v", st)
	}
	segs := segments(t, svc)
	if len(segs) != 3 {
		t.Fatalf("erwartet 3 Segmente, got %d: %+v", len(segs), segs)
	}
	w1, br, w2 := segs[0], segs[1], segs[2]
	if w1.Kind != domain.KindWork || w1.Open || !w1.End.Equal(at(start, 30*time.Minute)) {
		t.Errorf("work1: %+v", w1)
	}
	if br.Kind != domain.KindBreak || br.Open || br.Note != autoBreakNote ||
		!br.Start.Equal(at(start, 30*time.Minute)) || !br.End.Equal(at(start, 50*time.Minute)) {
		t.Errorf("auto-break: %+v", br)
	}
	if w2.Kind != domain.KindWork || !w2.Open || !w2.Start.Equal(at(start, 50*time.Minute)) {
		t.Errorf("work2: %+v", w2)
	}
	if len(w2.ProjectIDs) != 1 || w2.ProjectIDs[0] != w1.ProjectIDs[0] {
		t.Errorf("ProjectIDs nicht erhalten: %v vs %v", w2.ProjectIDs, w1.ProjectIDs)
	}
}

func TestLockSyncStillLockedThenUnlock(t *testing.T) {
	var events []ports.LockEvent
	var calls int
	svc, now := lockService(t, &events, &calls)
	start := *now
	if err := svc.Start("acme"); err != nil {
		t.Fatal(err)
	}
	events = []ports.LockEvent{{Time: at(start, 5*time.Minute), Locked: true}}
	*now = at(start, 25*time.Minute)
	st, err := svc.Status()
	if err != nil {
		t.Fatal(err)
	}
	if st.State != StatePaused {
		t.Fatalf("erwartet paused bei gesperrtem Schirm, got %v", st.State)
	}
	segs := segments(t, svc)
	if len(segs) != 2 || segs[1].Kind != domain.KindBreak || !segs[1].Open || segs[1].Note != autoBreakNote {
		t.Fatalf("offener Auto-Break fehlt: %+v", segs)
	}

	// Unlock kommt nach — nächster Sync resumed rückdatiert.
	events = append(events, ports.LockEvent{Time: at(start, 30*time.Minute), Locked: false})
	*now = at(start, 35*time.Minute)
	st, err = svc.Status()
	if err != nil {
		t.Fatal(err)
	}
	if st.State != StateWorking || !st.Since.Equal(at(start, 30*time.Minute)) {
		t.Errorf("Auto-Resume: %+v", st)
	}
	segs = segments(t, svc)
	if len(segs) != 3 || segs[1].Open || !segs[1].End.Equal(at(start, 30*time.Minute)) {
		t.Errorf("Break nicht am Unlock geschlossen: %+v", segs)
	}
}

func TestLockSyncShortLockIgnored(t *testing.T) {
	var events []ports.LockEvent
	var calls int
	svc, now := lockService(t, &events, &calls)
	start := *now
	if err := svc.Start("acme"); err != nil {
		t.Fatal(err)
	}
	// Unter domain.MinLegalBreak (ArbZG §4): zählt eh als Arbeitszeit, kein Eintrag.
	events = []ports.LockEvent{
		{Time: at(start, 20*time.Minute), Locked: true},
		{Time: at(start, 20*time.Minute+30*time.Second), Locked: false},
		{Time: at(start, 25*time.Minute), Locked: true},
		{Time: at(start, 35*time.Minute), Locked: false}, // 10 Min — auch zu kurz
	}
	*now = at(start, time.Hour)
	if _, err := svc.Status(); err != nil {
		t.Fatal(err)
	}
	segs := segments(t, svc)
	if len(segs) != 1 || !segs[0].Open {
		t.Errorf("Sperre unter 15 Min darf nichts ändern: %+v", segs)
	}
}

func TestLockSyncTrailingShortLockDeferred(t *testing.T) {
	var events []ports.LockEvent
	var calls int
	svc, now := lockService(t, &events, &calls)
	start := *now
	if err := svc.Start("acme"); err != nil {
		t.Fatal(err)
	}
	lock := at(start, 10*time.Minute)
	events = []ports.LockEvent{{Time: lock, Locked: true}}
	*now = at(start, 20*time.Minute) // Lock erst 10 Min alt
	if _, err := svc.Status(); err != nil {
		t.Fatal(err)
	}
	if segs := segments(t, svc); len(segs) != 1 {
		t.Errorf("vertagter Lock darf nichts ändern: %+v", segs)
	}
	wm, _ := svc.repo.GetConfig(lockSyncKey)
	if wm != strconv.FormatInt(lock.Unix(), 10) {
		t.Errorf("Watermark muss auf Lock-Zeit gedeckelt sein, got %q", wm)
	}
	// Später ist der Lock ≥15 Min alt → verarbeitet, rückdatiert.
	*now = at(start, 26*time.Minute)
	if _, err := svc.Status(); err != nil {
		t.Fatal(err)
	}
	segs := segments(t, svc)
	if len(segs) != 2 || segs[1].Kind != domain.KindBreak || !segs[1].Open || !segs[1].Start.Equal(lock) {
		t.Errorf("vertagter Lock nicht nachverarbeitet: %+v", segs)
	}
}

func TestLockSyncManualPauseUntouched(t *testing.T) {
	var events []ports.LockEvent
	var calls int
	svc, now := lockService(t, &events, &calls)
	start := *now
	if err := svc.Start("acme"); err != nil {
		t.Fatal(err)
	}
	*now = at(start, 30*time.Minute)
	if err := svc.Pause(); err != nil {
		t.Fatal(err)
	}
	events = []ports.LockEvent{
		{Time: at(start, 40*time.Minute), Locked: true},
		{Time: at(start, 50*time.Minute), Locked: false},
	}
	*now = at(start, time.Hour)
	st, err := svc.Status()
	if err != nil {
		t.Fatal(err)
	}
	if st.State != StatePaused {
		t.Errorf("manuelle Pause darf nicht auto-resumed werden: %v", st.State)
	}
	if segs := segments(t, svc); len(segs) != 2 || !segs[1].Open || segs[1].Note == autoBreakNote {
		t.Errorf("manuelle Pause verändert: %+v", segs)
	}
}

func TestLockSyncNoOpenEntryNoQuery(t *testing.T) {
	var events []ports.LockEvent
	var calls int
	svc, _ := lockService(t, &events, &calls)
	if _, err := svc.Status(); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Errorf("ohne offenes Segment darf die Quelle nicht befragt werden, calls=%d", calls)
	}
}

func TestLockSyncThrottle(t *testing.T) {
	var events []ports.LockEvent
	var calls int
	svc, now := lockService(t, &events, &calls)
	start := *now
	if err := svc.Start("acme"); err != nil {
		t.Fatal(err)
	}
	calls = 0
	*now = at(start, time.Minute)
	if _, err := svc.Status(); err != nil {
		t.Fatal(err)
	}
	*now = at(start, time.Minute+10*time.Second)
	if _, err := svc.Status(); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("zweiter Status binnen 30s muss gedrosselt sein, calls=%d", calls)
	}
	// Mutation erzwingt Sync trotz Drossel.
	if err := svc.Pause(); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("Pause muss Sync erzwingen, calls=%d", calls)
	}
}

func TestLockSyncTwoPairsOneBatch(t *testing.T) {
	var events []ports.LockEvent
	var calls int
	svc, now := lockService(t, &events, &calls)
	start := *now
	if err := svc.Start("acme"); err != nil {
		t.Fatal(err)
	}
	events = []ports.LockEvent{
		{Time: at(start, 10*time.Minute), Locked: true},
		{Time: at(start, 30*time.Minute), Locked: false},
		{Time: at(start, 45*time.Minute), Locked: true},
		{Time: at(start, 65*time.Minute), Locked: false},
	}
	*now = at(start, 90*time.Minute)
	if _, err := svc.Status(); err != nil {
		t.Fatal(err)
	}
	segs := segments(t, svc)
	if len(segs) != 5 {
		t.Fatalf("erwartet 5 Segmente, got %d: %+v", len(segs), segs)
	}
	wantKinds := []domain.Kind{domain.KindWork, domain.KindBreak, domain.KindWork, domain.KindBreak, domain.KindWork}
	for i, k := range wantKinds {
		if segs[i].Kind != k {
			t.Errorf("Segment %d: Kind %v, want %v", i, segs[i].Kind, k)
		}
	}
	if !segs[4].Open || !segs[4].Start.Equal(at(start, 65*time.Minute)) {
		t.Errorf("letztes work: %+v", segs[4])
	}
}

func TestLockSyncIdempotentReplay(t *testing.T) {
	var events []ports.LockEvent
	var calls int
	svc, now := lockService(t, &events, &calls)
	start := *now
	if err := svc.Start("acme"); err != nil {
		t.Fatal(err)
	}
	events = []ports.LockEvent{
		{Time: at(start, 10*time.Minute), Locked: true},
		{Time: at(start, 30*time.Minute), Locked: false},
	}
	*now = at(start, 40*time.Minute)
	if _, err := svc.Status(); err != nil {
		t.Fatal(err)
	}
	before := len(segments(t, svc))
	// Quelle liefert dieselben Events erneut (Slack-Replay).
	*now = at(start, 75*time.Minute)
	if _, err := svc.Status(); err != nil {
		t.Fatal(err)
	}
	if after := len(segments(t, svc)); after != before {
		t.Errorf("Replay hat Duplikate erzeugt: %d → %d", before, after)
	}
}

func TestLockSyncStopWhileAutoBreakOpen(t *testing.T) {
	var events []ports.LockEvent
	var calls int
	svc, now := lockService(t, &events, &calls)
	start := *now
	if err := svc.Start("acme"); err != nil {
		t.Fatal(err)
	}
	events = []ports.LockEvent{{Time: at(start, 5*time.Minute), Locked: true}}
	*now = at(start, 25*time.Minute)
	if _, err := svc.Status(); err != nil { // erzeugt offenen Auto-Break
		t.Fatal(err)
	}
	*now = at(start, 27*time.Minute)
	if _, err := svc.Stop(); err != nil {
		t.Fatal(err)
	}
	segs := segments(t, svc)
	for _, s := range segs {
		if s.Open {
			t.Errorf("nach Stop nichts offen erwartet: %+v", s)
		}
	}
	if last := segs[len(segs)-1]; last.Kind != domain.KindBreak || !last.End.Equal(at(start, 27*time.Minute)) {
		t.Errorf("Auto-Break muss bei Stop enden: %+v", last)
	}
}

func TestLockSyncLockAtStartSecond(t *testing.T) {
	var events []ports.LockEvent
	var calls int
	svc, now := lockService(t, &events, &calls)
	start := *now
	if err := svc.Start("acme"); err != nil {
		t.Fatal(err)
	}
	events = []ports.LockEvent{
		{Time: start, Locked: true},
		{Time: at(start, 20*time.Minute), Locked: false},
	}
	*now = at(start, 30*time.Minute)
	if _, err := svc.Status(); err != nil {
		t.Fatal(err)
	}
	segs := segments(t, svc)
	if len(segs) != 2 {
		t.Fatalf("work-Stub muss gelöscht sein: %+v", segs)
	}
	if segs[0].Kind != domain.KindBreak || !segs[0].Start.Equal(start) || !segs[0].End.Equal(at(start, 20*time.Minute)) {
		t.Errorf("Break: %+v", segs[0])
	}
}
