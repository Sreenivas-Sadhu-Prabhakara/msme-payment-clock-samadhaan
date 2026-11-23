package clock

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"
)

func TestParseDay(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		zero    bool
		wantErr bool
	}{
		{name: "plain date", in: "2026-04-02", want: "2026-04-02"},
		{name: "surrounding spaces", in: "  2026-04-02 ", want: "2026-04-02"},
		{name: "empty means not set", in: "", zero: true},
		{name: "blank means not set", in: "   ", zero: true},
		{name: "leap day in a leap year", in: "2024-02-29", want: "2024-02-29"},
		{name: "leap day in a common year", in: "2023-02-29", wantErr: true},
		{name: "day out of range", in: "2026-04-31", wantErr: true},
		{name: "month out of range", in: "2026-13-01", wantErr: true},
		{name: "wrong separator", in: "02/04/2026", wantErr: true},
		{name: "with a time", in: "2026-04-02T10:00:00Z", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseDay(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseDay(%q) = %v, want an error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseDay(%q) returned error: %v", tt.in, err)
			}
			if got.IsZero() != tt.zero {
				t.Fatalf("ParseDay(%q).IsZero() = %v, want %v", tt.in, got.IsZero(), tt.zero)
			}
			if got.String() != tt.want {
				t.Fatalf("ParseDay(%q).String() = %q, want %q", tt.in, got.String(), tt.want)
			}
		})
	}
}

func TestDayAddDays(t *testing.T) {
	tests := []struct {
		name string
		from string
		days int
		want string
	}{
		{name: "fifteen day period", from: "2026-04-02", days: NoAgreementDays, want: "2026-04-17"},
		{name: "forty-five day period", from: "2026-04-02", days: MaxAgreedDays, want: "2026-05-17"},
		{name: "forty-five days across February in a leap year", from: "2024-01-20", days: MaxAgreedDays, want: "2024-03-05"},
		{name: "forty-five days across February in a common year", from: "2023-01-20", days: MaxAgreedDays, want: "2023-03-06"},
		{name: "across a year boundary", from: "2025-12-20", days: MaxAgreedDays, want: "2026-02-03"},
		{name: "over the leap day itself", from: "2024-02-28", days: 1, want: "2024-02-29"},
		{name: "zero days is a no-op", from: "2026-04-02", days: 0, want: "2026-04-02"},
		{name: "negative days go back", from: "2026-03-01", days: -1, want: "2026-02-28"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			from := mustDay(t, tt.from)
			if got := from.AddDays(tt.days).String(); got != tt.want {
				t.Fatalf("%s.AddDays(%d) = %s, want %s", tt.from, tt.days, got, tt.want)
			}
		})
	}
}

func TestDayAddDaysOnZeroDayStaysUnset(t *testing.T) {
	if got := (Day{}).AddDays(45); !got.IsZero() {
		t.Fatalf("zero Day AddDays(45) = %s, want it to stay unset", got)
	}
}

func TestDayAddMonthsClampsToEndOfMonth(t *testing.T) {
	tests := []struct {
		name   string
		from   string
		months int
		want   string
	}{
		{name: "mid-month is untouched", from: "2026-01-15", months: 1, want: "2026-02-15"},
		{name: "31 January clamps to 28 February", from: "2023-01-31", months: 1, want: "2023-02-28"},
		{name: "31 January clamps to 29 February in a leap year", from: "2024-01-31", months: 1, want: "2024-02-29"},
		{name: "31 January to 31 March keeps the 31st", from: "2024-01-31", months: 2, want: "2024-03-31"},
		{name: "31 January to April clamps to the 30th", from: "2024-01-31", months: 3, want: "2024-04-30"},
		{name: "29 February clamps a year later", from: "2024-02-29", months: 12, want: "2025-02-28"},
		{name: "30 November to February", from: "2025-11-30", months: 3, want: "2026-02-28"},
		{name: "across a year boundary", from: "2025-12-31", months: 1, want: "2026-01-31"},
		{name: "twelve months", from: "2025-07-15", months: 12, want: "2026-07-15"},
		{name: "zero months is a no-op", from: "2025-07-15", months: 0, want: "2025-07-15"},
		{name: "negative months go back", from: "2025-03-31", months: -1, want: "2025-02-28"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			from := mustDay(t, tt.from)
			if got := from.AddMonths(tt.months).String(); got != tt.want {
				t.Fatalf("%s.AddMonths(%d) = %s, want %s", tt.from, tt.months, got, tt.want)
			}
		})
	}
}

func TestDayDaysUntil(t *testing.T) {
	tests := []struct {
		name string
		from string
		to   string
		want int
	}{
		{name: "same day", from: "2026-04-02", to: "2026-04-02", want: 0},
		{name: "one day", from: "2026-04-02", to: "2026-04-03", want: 1},
		{name: "backwards is negative", from: "2026-04-03", to: "2026-04-02", want: -1},
		{name: "February in a leap year", from: "2024-02-01", to: "2024-03-01", want: 29},
		{name: "February in a common year", from: "2023-02-01", to: "2023-03-01", want: 28},
		{name: "a full leap year", from: "2024-01-01", to: "2025-01-01", want: 366},
		{name: "a full common year", from: "2025-01-01", to: "2026-01-01", want: 365},
		{name: "forty-five days", from: "2026-04-02", to: "2026-05-17", want: MaxAgreedDays},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			from, to := mustDay(t, tt.from), mustDay(t, tt.to)
			if got := from.DaysUntil(to); got != tt.want {
				t.Fatalf("%s.DaysUntil(%s) = %d, want %d", tt.from, tt.to, got, tt.want)
			}
		})
	}
}

func TestDaysInMonth(t *testing.T) {
	tests := []struct {
		year  int
		month time.Month
		want  int
	}{
		{year: 2026, month: time.January, want: 31},
		{year: 2026, month: time.February, want: 28},
		{year: 2024, month: time.February, want: 29},
		{year: 2000, month: time.February, want: 29},
		{year: 1900, month: time.February, want: 28},
		{year: 2026, month: time.April, want: 30},
		{year: 2026, month: time.December, want: 31},
	}
	for _, tt := range tests {
		t.Run(tt.month.String()+" "+strconv.Itoa(tt.year), func(t *testing.T) {
			if got := DaysInMonth(tt.year, tt.month); got != tt.want {
				t.Fatalf("DaysInMonth(%d, %s) = %d, want %d", tt.year, tt.month, got, tt.want)
			}
		})
	}
}

func TestDayComparisons(t *testing.T) {
	a, b := NewDay(2026, time.April, 2), NewDay(2026, time.April, 3)
	if !a.Before(b) || a.After(b) || a.Equal(b) {
		t.Fatalf("expected %s to sort before %s", a, b)
	}
	if !a.Equal(NewDay(2026, time.April, 2)) {
		t.Fatal("expected equal dates to compare equal")
	}
}

func TestDayPretty(t *testing.T) {
	if got := NewDay(2026, time.April, 2).Pretty(); got != "02 April 2026" {
		t.Fatalf("Pretty() = %q, want %q", got, "02 April 2026")
	}
	if got := (Day{}).Pretty(); got != "—" {
		t.Fatalf("Pretty() on an unset date = %q, want an em dash", got)
	}
}

func TestDayOfDropsTheClockTime(t *testing.T) {
	ts := time.Date(2026, time.April, 2, 23, 59, 59, 0, time.FixedZone("IST", 5*3600+1800))
	if got := DayOf(ts).String(); got != "2026-04-02" {
		t.Fatalf("DayOf(%s) = %s, want 2026-04-02", ts, got)
	}
}

func TestDayJSONRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		day  Day
		json string
	}{
		{name: "a date", day: NewDay(2026, time.April, 2), json: `"2026-04-02"`},
		{name: "unset", day: Day{}, json: `""`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := json.Marshal(tt.day)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if string(b) != tt.json {
				t.Fatalf("Marshal = %s, want %s", b, tt.json)
			}
			var back Day
			if err := json.Unmarshal(b, &back); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if !back.Equal(tt.day) || back.IsZero() != tt.day.IsZero() {
				t.Fatalf("round trip gave %q, want %q", back, tt.day)
			}
		})
	}
}

func TestDayUnmarshalJSONRejectsRubbish(t *testing.T) {
	tests := []struct {
		name string
		in   string
		zero bool
	}{
		{name: "null means not set", in: `null`, zero: true},
		{name: "a number is not a date", in: `20260402`},
		{name: "a malformed date", in: `"02-04-2026"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d Day
			err := json.Unmarshal([]byte(tt.in), &d)
			if tt.zero {
				if err != nil || !d.IsZero() {
					t.Fatalf("Unmarshal(%s) = %q, %v; want an unset date and no error", tt.in, d, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Unmarshal(%s) = %q, want an error", tt.in, d)
			}
		})
	}
}

func mustDay(t *testing.T, s string) Day {
	t.Helper()
	d, err := ParseDay(s)
	if err != nil {
		t.Fatalf("ParseDay(%q): %v", s, err)
	}
	return d
}
