package domain

// FindOverlap prüft candidate gegen existing (halboffene Intervalle:
// berührende Segmente überlappen nicht). Segmente mit gleicher ID wie
// candidate werden übersprungen (Edit-Fall). Offene Segmente müssen vom
// Aufrufer mit End = now versehen sein.
func FindOverlap(candidate Segment, existing []Segment) *Segment {
	for _, e := range existing {
		if e.ID == candidate.ID {
			continue
		}
		if candidate.Start.Before(e.End) && e.Start.Before(candidate.End) {
			hit := e
			return &hit
		}
	}
	return nil
}
