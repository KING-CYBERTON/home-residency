package domain

import (
	"fmt"
	"time"

	"github.com/KING-CYBERTON/home-residency/backend/internal/httpx"
)

// Nairobi is where every visit happens; category hours are in its local time (UTC+3 all year).
var Nairobi = time.FixedZone("EAT", 3*3600)

// BookingWindow is when a category's visits may start: between OpensAt and ClosesAt ("HH:MM",
// Nairobi time, both empty for any time of day) and at least MinLead after the booking is made.
type BookingWindow struct {
	Category          string
	OpensAt, ClosesAt string
	MinLead           time.Duration
}

// CheckStart refuses a start time outside the category's hours or with too little notice.
func (b BookingWindow) CheckStart(start, now time.Time) *httpx.Error {
	if b.MinLead > 0 && start.Before(now.Add(b.MinLead)) {
		earliest := now.Add(b.MinLead).In(Nairobi)
		return httpx.RuleViolation(fmt.Sprintf("%s needs %s' notice: choose a start from %s", b.Category, notice(b.MinLead), earliest.Format("02/01/2006 15:04"))).
			With("earliest_start", earliest.Format(time.RFC3339))
	}
	if b.OpensAt == "" || b.ClosesAt == "" {
		return nil
	}
	local := start.In(Nairobi).Format("15:04")
	if local < b.OpensAt || local > b.ClosesAt {
		return httpx.RuleViolation(fmt.Sprintf("%s visits start between %s and %s", b.Category, b.OpensAt, b.ClosesAt)).
			With("opens_at", b.OpensAt).With("closes_at", b.ClosesAt)
	}
	return nil
}

func notice(d time.Duration) string {
	switch {
	case d%(24*time.Hour) == 0:
		return plural(int(d/(24*time.Hour)), "day")
	case d%time.Hour == 0:
		return plural(int(d/time.Hour), "hour")
	}
	return plural(int(d/time.Minute), "minute")
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}
