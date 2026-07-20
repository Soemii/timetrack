package cli

import (
	"fmt"
	"time"

	"timetrack/internal/core/domain"
)

// hm formatiert kompakt "H:MM".
func hm(d time.Duration) string {
	neg := ""
	if d < 0 {
		neg = "-"
		d = -d
	}
	d = d.Round(time.Minute)
	return fmt.Sprintf("%s%d:%02d", neg, int(d.Hours()), int(d.Minutes())%60)
}

// saldo formatiert mit Vorzeichen: "+3:20" / "-0:45" / "±0:00".
func saldo(d time.Duration) string {
	switch {
	case d > 0:
		return "+" + hm(d)
	case d < 0:
		return hm(d)
	default:
		return "±0:00"
	}
}

var weekdayShort = [7]string{"So", "Mo", "Di", "Mi", "Do", "Fr", "Sa"}

func dayLabel(d domain.Date) string {
	return fmt.Sprintf("%s %02d.%02d.", weekdayShort[d.Weekday()], d.Day, d.Month)
}

func absenceLabel(t domain.AbsenceType, fraction float64) string {
	name := map[domain.AbsenceType]string{
		domain.AbsenceUrlaub:   "Urlaub",
		domain.AbsenceKrank:    "Krank",
		domain.AbsenceFeiertag: "Feiertag",
	}[t]
	if fraction == 0.5 {
		return name + " (½)"
	}
	return name
}

func kindLabel(k domain.Kind) string {
	if k == domain.KindBreak {
		return "Pause"
	}
	return "Arbeit"
}
