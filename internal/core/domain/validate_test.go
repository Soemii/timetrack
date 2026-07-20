package domain

import "testing"

func TestFindOverlap(t *testing.T) {
	existing := []Segment{
		{ID: 1, Kind: KindWork, Start: ts("2026-07-20 09:00"), End: ts("2026-07-20 12:00")},
	}
	// berührend: kein Overlap
	c := Segment{ID: 0, Start: ts("2026-07-20 12:00"), End: ts("2026-07-20 13:00")}
	if hit := FindOverlap(c, existing); hit != nil {
		t.Errorf("berührende Intervalle dürfen nicht überlappen")
	}
	// 1 Minute drin: Overlap
	c = Segment{ID: 0, Start: ts("2026-07-20 11:59"), End: ts("2026-07-20 13:00")}
	if hit := FindOverlap(c, existing); hit == nil || hit.ID != 1 {
		t.Errorf("Overlap nicht erkannt")
	}
	// komplett umschließend: Overlap
	c = Segment{ID: 0, Start: ts("2026-07-20 08:00"), End: ts("2026-07-20 14:00")}
	if hit := FindOverlap(c, existing); hit == nil {
		t.Errorf("umschließender Overlap nicht erkannt")
	}
	// eigene ID wird übersprungen (Edit)
	c = Segment{ID: 1, Start: ts("2026-07-20 09:30"), End: ts("2026-07-20 12:30")}
	if hit := FindOverlap(c, existing); hit != nil {
		t.Errorf("eigenes Segment darf nicht als Overlap zählen")
	}
}
