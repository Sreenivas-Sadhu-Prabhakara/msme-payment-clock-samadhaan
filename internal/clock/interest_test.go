package clock

import (
	"math"
	"testing"
)

// tol is half a paise: interest is money, so expected values are asserted to
// the paise rather than to the last float bit.
const tol = 0.005

func TestInterest(t *testing.T) {
	// At the default Bank Rate of 6.50% the statutory rate is 19.50% a year,
	// so one monthly rest adds 19.50/12 = 1.625% of the balance.
	tests := []struct {
		name      string
		principal float64
		from      string
		to        string
		bankRate  float64
		want      float64
	}{
		{
			name:      "zero days accrues nothing",
			principal: 100000, from: "2026-01-01", to: "2026-01-01", bankRate: 6.50,
			want: 0,
		},
		{
			name:      "an end date before the start accrues nothing",
			principal: 100000, from: "2026-02-01", to: "2026-01-01", bankRate: 6.50,
			want: 0,
		},
		{
			name:      "exactly one month is one monthly rest",
			principal: 100000, from: "2026-01-01", to: "2026-02-01", bankRate: 6.50,
			want: 1625, // 100000 x 0.195 / 12
		},
		{
			name:      "a short February still counts as one whole month",
			principal: 100000, from: "2026-02-01", to: "2026-03-01", bankRate: 6.50,
			want: 1625,
		},
		{
			name:      "31 January to 29 February is one whole month in a leap year",
			principal: 100000, from: "2024-01-31", to: "2024-02-29", bankRate: 6.50,
			want: 1625,
		},
		{
			name:      "three months compound on each rest",
			principal: 100000, from: "2026-01-01", to: "2026-04-01", bankRate: 6.50,
			want: 4954.6478515625, // 100000 x (1.01625^3 - 1)
		},
		{
			name:      "a part month only, pro-rated over 365 days",
			principal: 100000, from: "2026-01-01", to: "2026-01-11", bankRate: 6.50,
			want: 534.2465753424658, // 100000 x 0.195 x 10 / 365
		},
		{
			name:      "one month plus a part month compounds then pro-rates",
			principal: 100000, from: "2026-01-01", to: "2026-02-11", bankRate: 6.50,
			want: 2167.9280821917808, // 1625 + 101625 x 0.195 x 10 / 365
		},
		{
			name:      "a single day",
			principal: 100000, from: "2026-01-01", to: "2026-01-02", bankRate: 6.50,
			want: 53.42465753424658, // 100000 x 0.195 / 365
		},
		{
			name:      "rests follow the start date, not the clamped rest",
			principal: 100000, from: "2024-01-31", to: "2024-03-31", bankRate: 6.50,
			want: 3276.40625, // two whole months: 100000 x (1.01625^2 - 1), no part days
		},
		{
			name:      "a different Bank Rate changes the rate",
			principal: 100000, from: "2026-01-01", to: "2026-02-01", bankRate: 5.75,
			want: 1437.5, // 100000 x (3 x 5.75 / 100) / 12
		},
		{
			name:      "a zero Bank Rate accrues nothing",
			principal: 100000, from: "2026-01-01", to: "2026-04-01", bankRate: 0,
			want: 0,
		},
		{
			name:      "zero principal accrues nothing",
			principal: 0, from: "2026-01-01", to: "2026-04-01", bankRate: 6.50,
			want: 0,
		},
		{
			name:      "a negative principal accrues nothing",
			principal: -5000, from: "2026-01-01", to: "2026-04-01", bankRate: 6.50,
			want: 0,
		},
		{
			name:      "a leap day inside the whole month is counted",
			principal: 100000, from: "2024-02-01", to: "2024-03-02", bankRate: 6.50,
			want: 1679.292808219178, // one rest on 1 March, then 1 day on 101625
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			from, to := mustDay(t, tt.from), mustDay(t, tt.to)
			got := Interest(tt.principal, from, to, tt.bankRate)
			if math.Abs(got-tt.want) > tol {
				t.Fatalf("Interest(%.2f, %s, %s, %.2f) = %.6f, want %.6f",
					tt.principal, tt.from, tt.to, tt.bankRate, got, tt.want)
			}
		})
	}
}

func TestInterestWithUnsetDatesAccruesNothing(t *testing.T) {
	day := mustDay(t, "2026-01-01")
	tests := []struct {
		name     string
		from, to Day
	}{
		{name: "no start date", to: day},
		{name: "no end date", from: day},
		{name: "neither date"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Interest(100000, tt.from, tt.to, BankRateDefaultPct); got != 0 {
				t.Fatalf("Interest with an unset date = %.6f, want 0", got)
			}
		})
	}
}

func TestInterestCompoundingExceedsSimpleInterest(t *testing.T) {
	const principal = 500000
	from := mustDay(t, "2026-01-01")
	tests := []struct {
		name   string
		to     string
		months int
	}{
		{name: "three months", to: "2026-04-01", months: 3},
		{name: "six months", to: "2026-07-01", months: 6},
		{name: "twelve months", to: "2027-01-01", months: 12},
		{name: "twenty-four months", to: "2028-01-01", months: 24},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			to := mustDay(t, tt.to)
			got := Interest(principal, from, to, BankRateDefaultPct)

			annual := AnnualRatePct(BankRateDefaultPct) / 100
			simple := principal * annual * float64(tt.months) / MonthsInYear
			if got <= simple {
				t.Fatalf("compound interest over %d months = %.2f, which does not exceed simple interest of %.2f",
					tt.months, got, simple)
			}

			// Monthly rests over whole months are the closed form
			// P((1 + r/12)^n - 1); the loop must agree with it.
			want := principal * (math.Pow(1+annual/MonthsInYear, float64(tt.months)) - 1)
			if math.Abs(got-want) > tol {
				t.Fatalf("compound interest over %d months = %.6f, want %.6f", tt.months, got, want)
			}
		})
	}
}

func TestInterestGrowsWithTime(t *testing.T) {
	from := mustDay(t, "2026-01-01")
	var prev float64
	for days := 1; days <= 400; days++ {
		got := Interest(250000, from, from.AddDays(days), BankRateDefaultPct)
		if got < prev {
			t.Fatalf("interest fell from %.6f to %.6f at day %d", prev, got, days)
		}
		prev = got
	}
	if prev <= 0 {
		t.Fatal("expected interest to have accrued over 400 days")
	}
}

func TestDescribe(t *testing.T) {
	tests := []struct {
		name       string
		from, to   string
		wantDays   int
		wantMonths int
		wantPart   int
	}{
		{name: "nothing accrued", from: "2026-01-01", to: "2026-01-01"},
		{name: "part month only", from: "2026-01-01", to: "2026-01-11", wantDays: 10, wantPart: 10},
		{name: "one whole month", from: "2026-01-01", to: "2026-02-01", wantDays: 31, wantMonths: 1},
		{name: "one month and ten days", from: "2026-01-01", to: "2026-02-11", wantDays: 41, wantMonths: 1, wantPart: 10},
		{name: "two months from the 31st", from: "2024-01-31", to: "2024-03-31", wantDays: 60, wantMonths: 2},
		{name: "a year and a day", from: "2026-01-01", to: "2027-01-02", wantDays: 366, wantMonths: 12, wantPart: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Describe(100000, mustDay(t, tt.from), mustDay(t, tt.to), BankRateDefaultPct)
			if got.Days != tt.wantDays || got.CompletedMonths != tt.wantMonths || got.PartMonthDays != tt.wantPart {
				t.Fatalf("Describe(%s..%s) = %d days, %d months, %d part days; want %d, %d, %d",
					tt.from, tt.to, got.Days, got.CompletedMonths, got.PartMonthDays,
					tt.wantDays, tt.wantMonths, tt.wantPart)
			}
			if got.AnnualRatePct != AnnualRatePct(BankRateDefaultPct) {
				t.Fatalf("Describe annual rate = %.4f, want %.4f", got.AnnualRatePct, AnnualRatePct(BankRateDefaultPct))
			}
			if want := Interest(100000, mustDay(t, tt.from), mustDay(t, tt.to), BankRateDefaultPct); math.Abs(got.Interest-want) > 1e-9 {
				t.Fatalf("Describe interest = %.6f, want %.6f from Interest", got.Interest, want)
			}
		})
	}
}

func TestAnnualRatePct(t *testing.T) {
	tests := []struct {
		bankRate float64
		want     float64
	}{
		{bankRate: 6.50, want: 19.50},
		{bankRate: 5.75, want: 17.25},
		{bankRate: 0, want: 0},
		{bankRate: 10, want: 30},
	}
	for _, tt := range tests {
		if got := AnnualRatePct(tt.bankRate); math.Abs(got-tt.want) > 1e-9 {
			t.Fatalf("AnnualRatePct(%.2f) = %.4f, want %.4f", tt.bankRate, got, tt.want)
		}
	}
}

func TestStatutoryConstants(t *testing.T) {
	if MaxAgreedDays != 45 {
		t.Fatalf("MaxAgreedDays = %d, want 45 (s.15 MSMED Act 2006)", MaxAgreedDays)
	}
	if NoAgreementDays != 15 {
		t.Fatalf("NoAgreementDays = %d, want 15 (s.15 MSMED Act 2006)", NoAgreementDays)
	}
	if InterestMultiple != 3 {
		t.Fatalf("InterestMultiple = %d, want 3 (s.16 MSMED Act 2006)", InterestMultiple)
	}
	if BankRateDefaultPct != 6.50 {
		t.Fatalf("BankRateDefaultPct = %.2f, want the documented default of 6.50", BankRateDefaultPct)
	}
	if DaysInYear != 365 {
		t.Fatalf("DaysInYear = %d, want 365", DaysInYear)
	}
}

func TestRound2(t *testing.T) {
	tests := []struct {
		in, want float64
	}{
		{in: 0, want: 0},
		{in: 1.004, want: 1.00},
		{in: 1.006, want: 1.01},
		{in: 1.0049999, want: 1.00},
		{in: 2.671, want: 2.67},
		{in: -1.006, want: -1.01},
		{in: 1234.5678, want: 1234.57},
		{in: 99999.999, want: 100000},
		{in: 1625, want: 1625},
	}
	for _, tt := range tests {
		if got := Round2(tt.in); math.Abs(got-tt.want) > 1e-9 {
			t.Fatalf("Round2(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestFormatINR(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{in: 0, want: "0.00"},
		{in: 1, want: "1.00"},
		{in: 999, want: "999.00"},
		{in: 1000, want: "1,000.00"},
		{in: 99999, want: "99,999.00"},
		{in: 100000, want: "1,00,000.00"},
		{in: 1250000, want: "12,50,000.00"},
		{in: 12345678.5, want: "1,23,45,678.50"},
		{in: 1234567890, want: "1,23,45,67,890.00"},
		{in: 1625.006, want: "1,625.01"},
		{in: 4954.6478515625, want: "4,954.65"},
		{in: -2500.5, want: "-2,500.50"},
		{in: 0.004, want: "0.00"},
		{in: -0.001, want: "0.00"},
	}
	for _, tt := range tests {
		if got := FormatINR(tt.in); got != tt.want {
			t.Fatalf("FormatINR(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
