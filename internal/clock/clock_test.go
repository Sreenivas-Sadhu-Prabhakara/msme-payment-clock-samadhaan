package clock

import (
	"math"
	"testing"
)

func TestAppointedDay(t *testing.T) {
	tests := []struct {
		name       string
		acceptance string
		agreed     string
		want       string
	}{
		{
			name:       "no agreement in writing gives fifteen days",
			acceptance: "2026-01-10", agreed: "",
			want: "2026-01-25",
		},
		{
			name:       "an agreed date inside forty-five days stands",
			acceptance: "2026-01-10", agreed: "2026-02-10",
			want: "2026-02-10",
		},
		{
			name:       "an agreed date beyond forty-five days is cut back to the cap",
			acceptance: "2026-01-10", agreed: "2026-04-10",
			want: "2026-02-24", // 10 January + 45 days
		},
		{
			name:       "an agreed date exactly on the cap stands",
			acceptance: "2026-01-10", agreed: "2026-02-24",
			want: "2026-02-24",
		},
		{
			name:       "one day past the cap is cut back",
			acceptance: "2026-01-10", agreed: "2026-02-25",
			want: "2026-02-24",
		},
		{
			name:       "an agreed date shorter than fifteen days stands",
			acceptance: "2026-01-10", agreed: "2026-01-17",
			want: "2026-01-17",
		},
		{
			name:       "the cap counts calendar days across a leap February",
			acceptance: "2024-01-20", agreed: "2024-12-31",
			want: "2024-03-05",
		},
		{
			name:       "the cap counts calendar days across a common February",
			acceptance: "2023-01-20", agreed: "2023-12-31",
			want: "2023-03-06",
		},
		{
			name:       "fifteen days across a year boundary",
			acceptance: "2025-12-25", agreed: "",
			want: "2026-01-09",
		},
		{
			name:       "no acceptance date means the clock has not started",
			acceptance: "", agreed: "2026-02-10",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AppointedDay(mustDay(t, tt.acceptance), mustDay(t, tt.agreed))
			if got.String() != tt.want {
				t.Fatalf("AppointedDay(%q, %q) = %q, want %q", tt.acceptance, tt.agreed, got, tt.want)
			}
			if tt.acceptance != "" {
				limit := mustDay(t, tt.acceptance).AddDays(MaxAgreedDays)
				if got.After(limit) {
					t.Fatalf("AppointedDay(%q, %q) = %q, which is past the %d-day cap of %q",
						tt.acceptance, tt.agreed, got, MaxAgreedDays, limit)
				}
			}
		})
	}
}

func TestAppointedDayNeverExceedsTheCap(t *testing.T) {
	acceptance := mustDay(t, "2026-01-10")
	limit := acceptance.AddDays(MaxAgreedDays)
	for days := 0; days <= 400; days++ {
		agreed := acceptance.AddDays(days)
		got := AppointedDay(acceptance, agreed)
		if got.After(limit) {
			t.Fatalf("an agreed date %d days out produced appointed day %q, past the cap %q", days, got, limit)
		}
		if days <= MaxAgreedDays && !got.Equal(agreed) {
			t.Fatalf("an agreed date %d days out produced %q, want the agreed date %q", days, got, agreed)
		}
	}
}

func TestInvoiceDaysOverdue(t *testing.T) {
	base := Invoice{
		No:             "INV-1",
		BuyerName:      "Buyer",
		Amount:         100000,
		AcceptanceDate: mustDay(t, "2026-01-10"), // appointed day 2026-01-25
	}
	tests := []struct {
		name string
		asOf string
		want int
	}{
		{name: "well before the appointed day", asOf: "2026-01-15", want: 0},
		{name: "on the appointed day", asOf: "2026-01-25", want: 0},
		{name: "the day after", asOf: "2026-01-26", want: 1},
		{name: "a month after", asOf: "2026-02-25", want: 31},
		{name: "a year after", asOf: "2027-01-25", want: 365},
		{name: "no as-of date", asOf: "", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := base.DaysOverdue(mustDay(t, tt.asOf)); got != tt.want {
				t.Fatalf("DaysOverdue(%q) = %d, want %d", tt.asOf, got, tt.want)
			}
		})
	}
}

func TestInvoiceOutstandingPrincipal(t *testing.T) {
	tests := []struct {
		name     string
		amount   float64
		received float64
		want     float64
	}{
		{name: "nothing received", amount: 100000, want: 100000},
		{name: "part received", amount: 100000, received: 40000, want: 60000},
		{name: "fully received", amount: 100000, received: 100000, want: 0},
		{name: "a stray paise short counts as settled", amount: 100000, received: 99999.999, want: 0},
		{name: "overpaid never goes negative", amount: 100000, received: 120000, want: 0},
		{name: "paise are kept", amount: 1000.55, received: 500.25, want: 500.30},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := Invoice{Amount: tt.amount, AmountReceived: tt.received}
			if got := inv.OutstandingPrincipal(); math.Abs(got-tt.want) > tol {
				t.Fatalf("OutstandingPrincipal() = %.4f, want %.4f", got, tt.want)
			}
		})
	}
}

func TestInvoiceStatus(t *testing.T) {
	tests := []struct {
		name         string
		inv          Invoice
		asOf         string
		wantState    State
		wantAppoint  string
		wantLeft     int
		wantOverdue  int
		wantOutstand float64
		wantInterest float64
		wantCapped   bool
	}{
		{
			name: "within time with no agreement",
			inv: Invoice{No: "A", Amount: 100000,
				AcceptanceDate: mustDay(t, "2026-01-10")},
			asOf: "2026-01-20", wantState: Within, wantAppoint: "2026-01-25",
			wantLeft: 5, wantOutstand: 100000,
		},
		{
			name: "the appointed day itself is still within time",
			inv: Invoice{No: "A", Amount: 100000,
				AcceptanceDate: mustDay(t, "2026-01-10")},
			asOf: "2026-01-25", wantState: Within, wantAppoint: "2026-01-25",
			wantLeft: 0, wantOutstand: 100000,
		},
		{
			name: "one day overdue carries one day of interest",
			inv: Invoice{No: "A", Amount: 100000,
				AcceptanceDate: mustDay(t, "2026-01-10")},
			asOf: "2026-01-26", wantState: Overdue, wantAppoint: "2026-01-25",
			wantOverdue: 1, wantOutstand: 100000, wantInterest: 53.42,
		},
		{
			name: "a month overdue is one monthly rest",
			inv: Invoice{No: "A", Amount: 100000,
				AcceptanceDate: mustDay(t, "2026-01-10")},
			asOf: "2026-02-25", wantState: Overdue, wantAppoint: "2026-01-25",
			wantOverdue: 31, wantOutstand: 100000, wantInterest: 1625,
		},
		{
			name: "an agreed date inside the cap moves the appointed day out",
			inv: Invoice{No: "A", Amount: 100000,
				AcceptanceDate:    mustDay(t, "2026-01-10"),
				AgreedPaymentDate: mustDay(t, "2026-02-10")},
			asOf: "2026-02-05", wantState: Within, wantAppoint: "2026-02-10",
			wantLeft: 5, wantOutstand: 100000,
		},
		{
			name: "an agreed date beyond the cap is read down and flagged",
			inv: Invoice{No: "A", Amount: 100000,
				AcceptanceDate:    mustDay(t, "2026-01-10"),
				AgreedPaymentDate: mustDay(t, "2026-04-10")},
			// 24 February to 24 March is one calendar month, so one rest.
			asOf: "2026-03-24", wantState: Overdue, wantAppoint: "2026-02-24",
			wantOverdue: 28, wantOutstand: 100000, wantInterest: 1625, wantCapped: true,
		},
		{
			name: "interest runs on the part still outstanding",
			inv: Invoice{No: "A", Amount: 100000,
				AcceptanceDate: mustDay(t, "2026-01-10"),
				AmountReceived: 40000, ReceivedDate: mustDay(t, "2026-02-01")},
			asOf: "2026-02-25", wantState: Overdue, wantAppoint: "2026-01-25",
			wantOverdue: 31, wantOutstand: 60000, wantInterest: 975,
		},
		{
			name: "settled on time carries no interest",
			inv: Invoice{No: "A", Amount: 100000,
				AcceptanceDate: mustDay(t, "2026-01-10"),
				AmountReceived: 100000, ReceivedDate: mustDay(t, "2026-01-20")},
			asOf: "2026-03-01", wantState: Settled, wantAppoint: "2026-01-25",
		},
		{
			name: "settled late still carries the interest that accrued",
			inv: Invoice{No: "A", Amount: 100000,
				AcceptanceDate: mustDay(t, "2026-01-10"),
				AmountReceived: 100000, ReceivedDate: mustDay(t, "2026-02-25")},
			asOf: "2026-06-01", wantState: Settled, wantAppoint: "2026-01-25",
			wantOverdue: 31, wantInterest: 1625,
		},
		{
			name: "no acceptance date leaves the clock unstarted",
			inv:  Invoice{No: "A", Amount: 100000},
			asOf: "2026-02-25", wantState: Within, wantAppoint: "",
			wantOutstand: 100000,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.inv.Status(mustDay(t, tt.asOf), BankRateDefaultPct)
			if got.State != tt.wantState {
				t.Errorf("State = %q, want %q", got.State, tt.wantState)
			}
			if got.AppointedDay.String() != tt.wantAppoint {
				t.Errorf("AppointedDay = %q, want %q", got.AppointedDay, tt.wantAppoint)
			}
			if got.DaysLeft != tt.wantLeft {
				t.Errorf("DaysLeft = %d, want %d", got.DaysLeft, tt.wantLeft)
			}
			if got.DaysOverdue != tt.wantOverdue {
				t.Errorf("DaysOverdue = %d, want %d", got.DaysOverdue, tt.wantOverdue)
			}
			if math.Abs(got.OutstandingPrincipal-tt.wantOutstand) > tol {
				t.Errorf("OutstandingPrincipal = %.2f, want %.2f", got.OutstandingPrincipal, tt.wantOutstand)
			}
			if math.Abs(got.Interest-tt.wantInterest) > tol {
				t.Errorf("Interest = %.2f, want %.2f", got.Interest, tt.wantInterest)
			}
			if got.CappedAt45Days != tt.wantCapped {
				t.Errorf("CappedAt45Days = %v, want %v", got.CappedAt45Days, tt.wantCapped)
			}
			if want := Round2(tt.wantOutstand + tt.wantInterest); math.Abs(got.TotalDue-want) > tol {
				t.Errorf("TotalDue = %.2f, want %.2f", got.TotalDue, want)
			}
			if got.State == Within && got.Interest != 0 {
				t.Errorf("an invoice within time carries interest of %.2f, want 0", got.Interest)
			}
		})
	}
}

func TestInvoiceStatusHonoursTheBankRateOverride(t *testing.T) {
	inv := Invoice{No: "A", Amount: 100000, AcceptanceDate: mustDay(t, "2026-01-10")}
	asOf := mustDay(t, "2026-02-25")

	tests := []struct {
		bankRate float64
		want     float64
	}{
		{bankRate: BankRateDefaultPct, want: 1625},
		{bankRate: 5.75, want: 1437.5},
		{bankRate: 9.00, want: 2250},
	}
	for _, tt := range tests {
		got := inv.Status(asOf, tt.bankRate)
		if math.Abs(got.Interest-tt.want) > tol {
			t.Fatalf("at a Bank Rate of %.2f%% interest = %.2f, want %.2f", tt.bankRate, got.Interest, tt.want)
		}
		if got.Period.BankRatePct != tt.bankRate || got.Period.AnnualRatePct != AnnualRatePct(tt.bankRate) {
			t.Fatalf("working shows %.2f%%/%.2f%%, want %.2f%%/%.2f%%",
				got.Period.BankRatePct, got.Period.AnnualRatePct, tt.bankRate, AnnualRatePct(tt.bankRate))
		}
	}
}

func TestTotals(t *testing.T) {
	invoices := []Invoice{
		// Overdue by 31 days on the full amount: interest 1625.00.
		{No: "A", BuyerName: "Buyer One", Amount: 100000, AcceptanceDate: mustDay(t, "2026-01-10")},
		// Within time: no interest.
		{No: "B", BuyerName: "Buyer One", Amount: 50000, AcceptanceDate: mustDay(t, "2026-02-20")},
		// Part paid, overdue by 31 days on 60000: interest 975.00.
		{No: "C", BuyerName: "Buyer Two", Amount: 100000, AcceptanceDate: mustDay(t, "2026-01-10"),
			AmountReceived: 40000, ReceivedDate: mustDay(t, "2026-02-01")},
		// Settled on time: nothing outstanding, no interest.
		{No: "D", BuyerName: "Buyer Two", Amount: 25000, AcceptanceDate: mustDay(t, "2026-01-10"),
			AmountReceived: 25000, ReceivedDate: mustDay(t, "2026-01-20")},
	}
	got := Totals(invoices, mustDay(t, "2026-02-25"), BankRateDefaultPct)

	if got.Count != 4 || got.Overdue != 2 || got.Within != 1 || got.Settled != 1 {
		t.Fatalf("counts = %d total, %d overdue, %d within, %d settled; want 4, 2, 1, 1",
			got.Count, got.Overdue, got.Within, got.Settled)
	}
	if want := 210000.0; math.Abs(got.OutstandingPrincipal-want) > tol {
		t.Fatalf("OutstandingPrincipal = %.2f, want %.2f", got.OutstandingPrincipal, want)
	}
	if want := 2600.0; math.Abs(got.Interest-want) > tol {
		t.Fatalf("Interest = %.2f, want %.2f", got.Interest, want)
	}
	if want := 212600.0; math.Abs(got.TotalDue-want) > tol {
		t.Fatalf("TotalDue = %.2f, want %.2f", got.TotalDue, want)
	}
	if got.BankRatePct != BankRateDefaultPct || got.AnnualRatePct != AnnualRatePct(BankRateDefaultPct) {
		t.Fatalf("Totals carried %.2f%%/%.2f%%, want the rate it was given", got.BankRatePct, got.AnnualRatePct)
	}
}

func TestTotalsOfNothing(t *testing.T) {
	got := Totals(nil, mustDay(t, "2026-02-25"), BankRateDefaultPct)
	if got.Count != 0 || got.OutstandingPrincipal != 0 || got.Interest != 0 || got.TotalDue != 0 {
		t.Fatalf("Totals(nil) = %+v, want everything zero", got)
	}
}

func TestBuyersAndForBuyer(t *testing.T) {
	invoices := []Invoice{
		{No: "A", BuyerName: "Zenith Works"},
		{No: "B", BuyerName: "  Acme Pumps  "},
		{No: "C", BuyerName: "Zenith Works"},
		{No: "D", BuyerName: ""},
		{No: "E", BuyerName: "acme pumps"},
	}

	buyers := Buyers(invoices)
	want := []string{"Acme Pumps", "Zenith Works", "acme pumps"}
	if len(buyers) != len(want) {
		t.Fatalf("Buyers() = %v, want %v", buyers, want)
	}
	for i := range want {
		if buyers[i] != want[i] {
			t.Fatalf("Buyers() = %v, want %v (sorted, de-duplicated, blanks dropped)", buyers, want)
		}
	}

	tests := []struct {
		name  string
		buyer string
		want  []string
	}{
		{name: "exact name", buyer: "Zenith Works", want: []string{"A", "C"}},
		{name: "ignores case and spacing", buyer: "  acme PUMPS ", want: []string{"B", "E"}},
		{name: "unknown buyer", buyer: "Nobody", want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ForBuyer(invoices, tt.buyer)
			if len(got) != len(tt.want) {
				t.Fatalf("ForBuyer(%q) matched %d invoices, want %d", tt.buyer, len(got), len(tt.want))
			}
			for i, no := range tt.want {
				if got[i].No != no {
					t.Fatalf("ForBuyer(%q)[%d] = %q, want %q", tt.buyer, i, got[i].No, no)
				}
			}
		})
	}
}

func TestValidateGSTIN(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{name: "a well-formed GSTIN", in: "29ABCDE1234F1Z5"},
		{name: "lower case is accepted and normalised", in: "29abcde1234f1z5"},
		{name: "spaces are trimmed", in: " 29ABCDE1234F1Z5 "},
		{name: "empty is allowed", in: ""},
		{name: "too short", in: "29ABCDE1234F1Z", wantErr: true},
		{name: "too long", in: "29ABCDE1234F1Z55", wantErr: true},
		{name: "state code must be digits", in: "AAABCDE1234F1Z5", wantErr: true},
		{name: "the PAN block must be five letters", in: "291BCDE1234F1Z5", wantErr: true},
		{name: "the PAN block must hold four digits", in: "29ABCDEX234F1Z5", wantErr: true},
		{name: "punctuation is rejected", in: "29ABCDE1234F1Z-", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGSTIN(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateGSTIN(%q) error = %v, want an error: %v", tt.in, err, tt.wantErr)
			}
		})
	}
}

func TestValidateUdyam(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{name: "a well-formed number", in: "UDYAM-KR-03-0001234"},
		{name: "lower case is accepted", in: "udyam-kr-03-0001234"},
		{name: "empty is allowed", in: ""},
		{name: "missing the state code", in: "UDYAM-03-0001234", wantErr: true},
		{name: "too few digits in the serial", in: "UDYAM-KR-03-1234", wantErr: true},
		{name: "no prefix", in: "KR-03-0001234", wantErr: true},
		{name: "wrong separators", in: "UDYAM/KR/03/0001234", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateUdyam(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateUdyam(%q) error = %v, want an error: %v", tt.in, err, tt.wantErr)
			}
		})
	}
}

func TestInvoiceValidate(t *testing.T) {
	good := func() Invoice {
		return Invoice{
			No:             "INV-2026-001",
			Date:           mustDay(t, "2026-01-05"),
			AcceptanceDate: mustDay(t, "2026-01-10"),
			Amount:         100000,
			BuyerName:      "Acme Pumps Pvt Ltd",
			BuyerGSTIN:     "29ABCDE1234F1Z5",
		}
	}
	tests := []struct {
		name    string
		mutate  func(*Invoice)
		wantErr bool
	}{
		{name: "a complete invoice", mutate: func(*Invoice) {}},
		{name: "no GSTIN is fine", mutate: func(i *Invoice) { i.BuyerGSTIN = "" }},
		{name: "no invoice date is fine", mutate: func(i *Invoice) { i.Date = Day{} }},
		{name: "a part payment with a date is fine", mutate: func(i *Invoice) {
			i.AmountReceived = 40000
			i.ReceivedDate = mustDay(t, "2026-02-01")
		}},
		{name: "missing invoice number", mutate: func(i *Invoice) { i.No = "  " }, wantErr: true},
		{name: "missing buyer", mutate: func(i *Invoice) { i.BuyerName = "" }, wantErr: true},
		{name: "zero amount", mutate: func(i *Invoice) { i.Amount = 0 }, wantErr: true},
		{name: "negative amount", mutate: func(i *Invoice) { i.Amount = -1 }, wantErr: true},
		{name: "missing acceptance date", mutate: func(i *Invoice) { i.AcceptanceDate = Day{} }, wantErr: true},
		{name: "acceptance before the invoice date", mutate: func(i *Invoice) {
			i.AcceptanceDate = mustDay(t, "2026-01-01")
		}, wantErr: true},
		{name: "agreed date before acceptance", mutate: func(i *Invoice) {
			i.AgreedPaymentDate = mustDay(t, "2026-01-09")
		}, wantErr: true},
		{name: "negative receipt", mutate: func(i *Invoice) { i.AmountReceived = -5 }, wantErr: true},
		{name: "receipt above the invoice amount", mutate: func(i *Invoice) {
			i.AmountReceived = 100001
			i.ReceivedDate = mustDay(t, "2026-02-01")
		}, wantErr: true},
		{name: "a receipt with no date", mutate: func(i *Invoice) { i.AmountReceived = 40000 }, wantErr: true},
		{name: "received before acceptance", mutate: func(i *Invoice) {
			i.AmountReceived = 40000
			i.ReceivedDate = mustDay(t, "2026-01-09")
		}, wantErr: true},
		{name: "a malformed GSTIN", mutate: func(i *Invoice) { i.BuyerGSTIN = "29ABCDE" }, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := good()
			tt.mutate(&inv)
			err := inv.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, want an error: %v", err, tt.wantErr)
			}
		})
	}
}
