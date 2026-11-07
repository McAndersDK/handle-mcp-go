package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// ErrNotATime means the text is not an ISO date or time. Stores sometimes send
// text there ("03/11-2026 kl. 14-21"); the adapter turns this into a missing value.
var ErrNotATime = errors.New("not an ISO time")

// Date is a calendar day. It marshals as 2026-11-03.
type Date struct{ y, m, d int }

// NewDate makes a Date and checks that the day exists.
func NewDate(year int, month time.Month, day int) (Date, error) {
	t := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	if t.Year() != year || t.Month() != month || t.Day() != day || year < 1 || year > 9999 {
		return Date{}, ErrNotATime
	}
	return Date{year, int(month), day}, nil
}

// ParseDate reads 2026-11-03 or 20261103.
func ParseDate(s string) (Date, error) {
	y, m, d, ok := splitDate(s)
	if !ok || len(s) != dateLen(s) {
		return Date{}, ErrNotATime
	}
	return NewDate(y, time.Month(m), d)
}

// YMD returns the parts.
func (d Date) YMD() (int, time.Month, int) { return d.y, time.Month(d.m), d.d }

// String is Python's date.isoformat().
func (d Date) String() string { return fmt.Sprintf("%04d-%02d-%02d", d.y, d.m, d.d) }

// MarshalJSON writes the ISO string.
func (d Date) MarshalJSON() ([]byte, error) { return json.Marshal(d.String()) }

// UnmarshalJSON reads an ISO date string.
func (d *Date) UnmarshalJSON(b []byte) error {
	var s *string
	if err := json.Unmarshal(b, &s); err != nil || s == nil {
		return err
	}
	v, err := ParseDate(*s)
	if err != nil {
		return err
	}
	*d = v
	return nil
}

// DateTime is a point in time that may have no zone. A naive time stays naive and
// prints without an offset, like Python's datetime.isoformat(); an aware one keeps
// its offset (+01:00).
type DateTime struct {
	t     time.Time
	aware bool
}

// NewDateTime makes an aware DateTime. Build wall-clock delivery times with time.Date
// in the Copenhagen location, never by adding hours.
func NewDateTime(t time.Time) DateTime { return DateTime{t: t, aware: true} }

// NaiveDateTime makes a DateTime with no zone from the wall-clock parts of t.
func NaiveDateTime(t time.Time) DateTime {
	y, m, d := t.Date()
	return DateTime{t: time.Date(y, m, d, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)}
}

// Time returns the instant. For a naive value the zone is UTC and means nothing.
func (v DateTime) Time() time.Time { return v.t }

// Aware tells whether the value has an offset.
func (v DateTime) Aware() bool { return v.aware }

// Date is the calendar day in the value's own zone.
func (v DateTime) Date() Date { return Date{v.t.Year(), int(v.t.Month()), v.t.Day()} }

// String is Python's datetime.isoformat(): microseconds only when not zero.
func (v DateTime) String() string {
	s := v.t.Format("2006-01-02T15:04:05")
	if us := v.t.Nanosecond() / 1000; us != 0 {
		s += fmt.Sprintf(".%06d", us)
	}
	if !v.aware {
		return s
	}
	_, off := v.t.Zone()
	sign := "+"
	if off < 0 {
		sign, off = "-", -off
	}
	s += fmt.Sprintf("%s%02d:%02d", sign, off/3600, off%3600/60)
	if off%60 != 0 {
		s += fmt.Sprintf(":%02d", off%60)
	}
	return s
}

// MarshalJSON writes the ISO string.
func (v DateTime) MarshalJSON() ([]byte, error) { return json.Marshal(v.String()) }

// UnmarshalJSON reads an ISO string; text that is not a time is an error.
func (v *DateTime) UnmarshalJSON(b []byte) error {
	var s *string
	if err := json.Unmarshal(b, &s); err != nil || s == nil {
		return err
	}
	p, err := ParseDateTime(*s)
	if err != nil {
		return err
	}
	*v = p
	return nil
}

// ParseDateTime reads what Python's datetime.fromisoformat reads for store data: a date,
// optionally a separator and HH[:MM[:SS[.f]]], optionally Z or an offset. Fraction digits
// beyond six are cut off (the 7-digit .NET form). Anything else is ErrNotATime.
func ParseDateTime(s string) (DateTime, error) {
	y, m, d, ok := splitDate(s)
	if !ok {
		return DateTime{}, ErrNotATime
	}
	if _, err := NewDate(y, time.Month(m), d); err != nil {
		return DateTime{}, err
	}
	rest := s[dateLen(s):]
	hh, mi, ss, ns := 0, 0, 0, 0
	loc := time.UTC
	aware := false
	if rest != "" {
		_, n := utf8.DecodeRuneInString(rest) // any one character may separate date and time
		clock, zone := splitZone(rest[n:])
		var ok2 bool
		if hh, mi, ss, ns, ok2 = parseClock(clock); !ok2 {
			return DateTime{}, ErrNotATime
		}
		if zone != "" {
			off, ok3 := parseOffset(zone)
			if !ok3 {
				return DateTime{}, ErrNotATime
			}
			aware = true
			loc = time.UTC
			if off != 0 {
				loc = time.FixedZone("", off)
			}
		}
	}
	return DateTime{t: time.Date(y, time.Month(m), d, hh, mi, ss, ns, loc), aware: aware}, nil
}

// dateLen is 10 for 2026-11-03 and 8 for 20261103.
func dateLen(s string) int {
	if len(s) >= 10 && s[4] == '-' {
		return 10
	}
	return 8
}

func splitDate(s string) (y, m, d int, ok bool) {
	var a, b, c string
	switch {
	case len(s) >= 10 && s[4] == '-' && s[7] == '-':
		a, b, c = s[0:4], s[5:7], s[8:10]
	case len(s) >= 8 && isDigits(s[:8]):
		a, b, c = s[0:4], s[4:6], s[6:8]
	default:
		return 0, 0, 0, false
	}
	if !isDigits(a) || !isDigits(b) || !isDigits(c) {
		return 0, 0, 0, false
	}
	y, _ = strconv.Atoi(a)
	m, _ = strconv.Atoi(b)
	d, _ = strconv.Atoi(c)
	return y, m, d, true
}

// splitZone cuts a trailing Z or +HH[:MM] off a clock text.
func splitZone(s string) (clock, zone string) {
	if strings.HasSuffix(s, "Z") || strings.HasSuffix(s, "z") {
		return s[:len(s)-1], "Z"
	}
	if i := strings.IndexAny(s, "+-"); i >= 0 {
		return s[:i], s[i:]
	}
	return s, ""
}

// parseClock reads HH, HH:MM, HH:MM:SS[.f] or HHMM[SS[.f]]. The fraction may be
// separated by "." or ",".
func parseClock(s string) (h, m, sec, ns int, ok bool) {
	hasFrac := false
	if i := strings.IndexAny(s, ".,"); i >= 0 {
		hasFrac = true
		frac := s[i+1:]
		if frac == "" || !isDigits(frac) {
			return 0, 0, 0, 0, false
		}
		if len(frac) > 6 {
			frac = frac[:6]
		}
		frac += strings.Repeat("0", 9-len(frac))
		ns, _ = strconv.Atoi(frac)
		s = s[:i]
	}
	parts := []string{s}
	if strings.Contains(s, ":") {
		parts = strings.Split(s, ":")
	} else if len(s) > 2 && len(s)%2 == 0 {
		parts = nil
		for i := 0; i < len(s); i += 2 {
			parts = append(parts, s[i:i+2])
		}
	}
	if len(parts) > 3 || (hasFrac && len(parts) < 3) {
		return 0, 0, 0, 0, false
	}
	var v [3]int
	for i, p := range parts {
		if len(p) != 2 || !isDigits(p) {
			return 0, 0, 0, 0, false
		}
		v[i], _ = strconv.Atoi(p)
	}
	if v[0] > 23 || v[1] > 59 || v[2] > 59 {
		return 0, 0, 0, 0, false
	}
	return v[0], v[1], v[2], ns, true
}

// parseOffset reads Z, +HH, +HH:MM, +HHMM and +HH:MM:SS as seconds east of UTC.
func parseOffset(z string) (int, bool) {
	if z == "Z" {
		return 0, true
	}
	sign := 1
	if z[0] == '-' {
		sign = -1
	}
	h, m, s, _, ok := parseClock(z[1:])
	if !ok || h > 23 || strings.ContainsAny(z[1:], ".,") {
		return 0, false
	}
	return sign * (h*3600 + m*60 + s), true
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
