package domain

import (
	"strings"
	"testing"
	"time"
)

func TestBookingWindow(t *testing.T) {
	w := BookingWindow{Category: "Cleaning", OpensAt: "07:00", ClosesAt: "19:00", MinLead: 2 * time.Hour}
	at := func(s string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", s, Nairobi)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	now := at("2026-10-24 09:00")
	for _, c := range []struct {
		start string
		want  string // "" for allowed
	}{
		{"2026-10-24 11:00", ""},                              // exactly the notice
		{"2026-10-24 10:59", "needs 2 hours' notice"},         // a minute short
		{"2026-10-25 07:00", ""},                              // opening time
		{"2026-10-25 19:00", ""},                              // latest start
		{"2026-10-25 19:01", "start between 07:00 and 19:00"}, // after hours
		{"2026-10-25 06:59", "start between 07:00 and 19:00"}, // before hours
		{"2026-10-25 02:00", "start between 07:00 and 19:00"}, // the middle of the night
	} {
		err := w.CheckStart(at(c.start), now)
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s: refused: %v", c.start, err)
		case c.want != "" && (err == nil || !strings.Contains(err.Message, c.want)):
			t.Errorf("%s: got %v, want %q", c.start, err, c.want)
		}
	}
	// Hours are Nairobi time whatever zone the start arrives in: 16:30 UTC is 19:30 in Nairobi.
	if err := w.CheckStart(time.Date(2026, 10, 25, 16, 30, 0, 0, time.UTC), now); err == nil {
		t.Error("19:30 Nairobi time should be after hours")
	}
	// No hours and no notice: any time.
	if err := (BookingWindow{Category: "Plumbing"}).CheckStart(at("2026-10-24 03:00"), now.Add(-time.Hour)); err != nil {
		t.Errorf("unrestricted: %v", err)
	}
	if got := notice(24 * time.Hour); got != "1 day" {
		t.Errorf("notice: %q", got)
	}
	if got := notice(90 * time.Minute); got != "90 minutes" {
		t.Errorf("notice: %q", got)
	}
}
