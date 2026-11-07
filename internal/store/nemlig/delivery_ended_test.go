package nemlig

import (
	"context"
	"strings"
	"testing"
	"time"
)

// A chosen slot that has ended is no chosen slot (as at BilkaToGo): a past delivery day would make
// compare count an expired offer as active. An unreadable slot is kept, not hidden.
func TestDeliveryInfoHidesEndedSlot(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	spot := func(start, end string) string {
		return `{"TimeSlot": {"Start": "` + start + `", "End": "` + end + `"}, "EditDeadline": "2026-11-06T20:00:00+00:00"}`
	}
	cases := []struct {
		name, spot string
		hidden     bool
	}{
		{"ended", spot("2026-11-03T16:00:00+00:00", "2026-11-03T19:00:00+00:00"), true},
		{"ended a second ago", spot("2026-11-07T09:00:00+00:00", "2026-11-07T11:59:59+00:00"), true},
		{"ends exactly now", spot("2026-11-07T09:00:00+00:00", "2026-11-07T12:00:00+00:00"), false},
		{"future", spot("2026-11-09T16:00:00+00:00", "2026-11-09T19:00:00+00:00"), false},
		{"no zone, ends exactly now in Danish time", spot("2026-11-07T10:00:00", "2026-11-07T13:00:00"), false},
		{"no zone, ended a second ago in Danish time", spot("2026-11-07T10:00:00", "2026-11-07T12:59:59"), true},
		{"ended, no zone, yesterday", spot("2026-11-06T16:00:00", "2026-11-06T19:00:00"), true},
		{"unreadable end", spot("2026-11-03T16:00:00+00:00", "ikke en tid"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h, s := envLogin(t)
			h.clock = now
			basketWith(`{"FormattedDeliveryTime": "tirsdag 3. nov. 17-20"}`)(h)
			bodyAt(pSpot, c.spot)(h)
			info, err := s.DeliveryInfo(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			note := ""
			if info.Note != nil {
				note = *info.Note
			}
			if c.hidden {
				if info.ChosenStart != nil || info.ChosenEnd != nil || info.EditDeadline != nil {
					t.Errorf("an ended slot must be hidden: %v %v %v", info.ChosenStart, info.ChosenEnd, info.EditDeadline)
				}
				if !strings.HasPrefix(note, "Ingen tid valgt") || strings.Contains(note, "tirsdag") {
					t.Errorf("note %q", note)
				}
				return
			}
			if info.ChosenStart == nil || !strings.HasPrefix(note, "Valgt tid i kurven: tirsdag") {
				t.Errorf("a slot that has not ended must stay: start %v, note %q", info.ChosenStart, note)
			}
		})
	}
}
