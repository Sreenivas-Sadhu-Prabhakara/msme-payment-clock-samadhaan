package clock

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// mentions reports whether a document contains a phrase, ignoring the line
// breaks that wrapping inserts: the documents are wrapped to a paper width, so
// a cited phrase may straddle two lines.
func mentions(doc, phrase string) bool {
	if strings.Contains(doc, phrase) {
		return true
	}
	oneLine := strings.Join(strings.Fields(doc), " ")
	return strings.Contains(oneLine, strings.Join(strings.Fields(phrase), " "))
}

func testSupplier() Supplier {
	return Supplier{
		Name:        "Vishwakarma Precision Works",
		UdyamNumber: "UDYAM-KR-03-0001234",
		Address:     "Plot 14, Peenya Industrial Area\nBengaluru 560058",
		Contact:     "+91 98450 00000",
		Email:       "accounts@vpw.example",
	}
}

// testInvoices covers the four shapes the documents have to handle: unpaid with
// no written agreement, part paid under an over-long agreed date, settled late,
// and another buyer's invoice that must never leak into a packet.
func testInvoices(t *testing.T) []Invoice {
	t.Helper()
	return []Invoice{
		{ID: "1", No: "VPW/2026/041", Date: mustDay(t, "2026-01-05"), AcceptanceDate: mustDay(t, "2026-01-10"),
			Amount: 486500, BuyerName: "Nandi Agro Equipment Ltd", BuyerGSTIN: "29ABCDE1234F1Z5"},
		{ID: "2", No: "VPW/2026/052", Date: mustDay(t, "2026-01-20"), AcceptanceDate: mustDay(t, "2026-01-22"),
			Amount: 1250000, BuyerName: "Nandi Agro Equipment Ltd", BuyerGSTIN: "29ABCDE1234F1Z5",
			AgreedPaymentDate: mustDay(t, "2026-05-30"), AmountReceived: 400000, ReceivedDate: mustDay(t, "2026-04-02")},
		{ID: "3", No: "VPW/2026/060", Date: mustDay(t, "2026-02-02"), AcceptanceDate: mustDay(t, "2026-02-05"),
			Amount: 90000, BuyerName: "Nandi Agro Equipment Ltd",
			AmountReceived: 90000, ReceivedDate: mustDay(t, "2026-04-10")},
		{ID: "4", No: "OTHER/1", Date: mustDay(t, "2026-02-02"), AcceptanceDate: mustDay(t, "2026-02-05"),
			Amount: 12000, BuyerName: "Unrelated Traders"},
	}
}

func TestReminderLetter(t *testing.T) {
	sup := testSupplier()
	invs := testInvoices(t)
	asOf := mustDay(t, "2026-06-15")

	tests := []struct {
		name     string
		inv      Invoice
		contains []string
		absent   []string
	}{
		{
			name: "overdue invoice",
			inv:  invs[0],
			contains: []string{
				"Vishwakarma Precision Works",
				"Udyam Registration No.: UDYAM-KR-03-0001234",
				"Nandi Agro Equipment Ltd",
				"GSTIN: 29ABCDE1234F1Z5",
				"VPW/2026/041",
				"4,86,500.00",
				"25 January 2026", // the appointed day
				"141 day(s) past",
				"Date: 15 June 2026",
			},
			absent: []string{"section 16", "Notice of demand", "Facilitation Council under section 18"},
		},
		{
			name: "part paid invoice acknowledges what came in",
			inv:  invs[1],
			contains: []string{
				"VPW/2026/052",
				"We have received",
				"4,00,000.00",
				"02 April 2026",
				"08 March 2026", // 45-day cap on the agreed date
			},
		},
		{
			name: "settled invoice asks for a reconciliation, not money",
			inv:  invs[2],
			contains: []string{
				"settled in full on 10 April 2026",
			},
			absent: []string{"is still outstanding", "invoke the interest"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ReminderLetter(sup, tt.inv, asOf, BankRateDefaultPct)
			for _, want := range tt.contains {
				if !mentions(got, want) {
					t.Errorf("reminder letter is missing %q\n---\n%s", want, got)
				}
			}
			for _, unwanted := range tt.absent {
				if mentions(got, unwanted) {
					t.Errorf("reminder letter should not mention %q\n---\n%s", unwanted, got)
				}
			}
		})
	}
}

func TestFormalNoticeCitesTheSectionsAndShowsItsWorking(t *testing.T) {
	sup := testSupplier()
	invs := testInvoices(t)
	asOf := mustDay(t, "2026-06-15")

	got := FormalNotice(sup, invs[1], asOf, BankRateDefaultPct)
	want := []string{
		"Notice of demand",
		"sections 15 and 16 of the MSMED Act 2006",
		"Section 15 of the Micro, Small and Medium Enterprises Development Act 2006",
		"may in no case exceed forty-five days",
		"Section 16 of the Act",
		"compound interest, with",
		"monthly rests",
		"three times the Bank Rate",
		"Bank Rate of 6.50% per annum",
		"19.50% per annum",
		"08 March 2026",   // the appointed day after the 45-day cap
		"read down to 45", // the cap was applied and said so
		"8,50,000.00",     // principal outstanding
		"45,450.77",       // interest to the as-of date
		"8,95,450.77",     // total
		"within 15 days of receipt",
		"section 18 of the Act",
		"MSME Samadhaan portal",
		"section 43B(h) of the Income-tax Act 1961",
		"3 completed month(s), compounded",
		"7 day(s), simple, on a 365-day year",
		"Authorised signatory",
	}
	for _, w := range want {
		if !mentions(got, w) {
			t.Errorf("formal notice is missing %q\n---\n%s", w, got)
		}
	}
}

func TestFormalNoticeWithoutAWrittenAgreementCitesTheFifteenDayLimb(t *testing.T) {
	invs := testInvoices(t)
	got := FormalNotice(testSupplier(), invs[0], mustDay(t, "2026-06-15"), BankRateDefaultPct)

	if !mentions(got, "no agreement in writing") {
		t.Errorf("notice should say there is no agreement in writing\n---\n%s", got)
	}
	if !mentions(got, "fifteen days from the day of acceptance") {
		t.Errorf("notice should cite the fifteen-day limb of s.15\n---\n%s", got)
	}
	if mentions(got, "read down to") {
		t.Errorf("notice should not claim a cap was applied when no date was agreed\n---\n%s", got)
	}
}

func TestFormalNoticeFollowsTheBankRateOverride(t *testing.T) {
	invs := testInvoices(t)
	got := FormalNotice(testSupplier(), invs[0], mustDay(t, "2026-06-15"), 5.75)

	for _, want := range []string{"Bank Rate of 5.75% per annum", "17.25% per annum"} {
		if !mentions(got, want) {
			t.Errorf("notice is missing %q after a Bank Rate override\n---\n%s", want, got)
		}
	}
	if mentions(got, "19.50% per annum") {
		t.Errorf("notice still quotes the default rate after an override\n---\n%s", got)
	}
}

func TestSamadhaanPacket(t *testing.T) {
	sup := testSupplier()
	invs := testInvoices(t)
	asOf := mustDay(t, "2026-06-15")

	got := SamadhaanPacket(sup, "Nandi Agro Equipment Ltd", invs, asOf, BankRateDefaultPct)

	want := []string{
		"MSME SAMADHAAN — WORKING PAPER FOR A REFERENCE UNDER SECTION 18",
		"Prepared on 15 June 2026",
		"APPLICANT (SUPPLIER)",
		"UDYAM-KR-03-0001234",
		"Plot 14, Peenya Industrial Area, Bengaluru 560058",
		"RESPONDENT (BUYER)",
		"Nandi Agro Equipment Ltd",
		"29ABCDE1234F1Z5",
		"SCHEDULE OF INVOICES CLAIMED",
		"VPW/2026/041",
		"VPW/2026/052",
		"AMOUNT CLAIMED",
		"13,36,500.00", // 4,86,500 + 8,50,000 principal
		"83,674.10",    // 38,223.33 + 45,450.77 interest
		"14,20,174.10", // total claimed
		"INVOICES ALREADY SETTLED",
		"VPW/2026/060",
		"BASIS OF COMPUTATION",
		"three times the Bank Rate",
		"monthly rests",
		"45 days from acceptance",
		"15 days from the day of acceptance",
		"Section 2(b)",
		"registered on the Udyam portal",
		"DOCUMENTS TO ATTACH",
		"Udyam Registration Certificate",
		"proof of delivery",
		"Nothing has been sent, emailed or filed",
	}
	for _, w := range want {
		if !mentions(got, w) {
			t.Errorf("Samadhaan packet is missing %q\n---\n%s", w, got)
		}
	}

	if strings.Contains(got, "OTHER/1") || strings.Contains(got, "Unrelated Traders") {
		t.Errorf("Samadhaan packet leaked another buyer's invoice\n---\n%s", got)
	}

	// The settled invoice must not be claimed as principal.
	claimed := got[strings.Index(got, "AMOUNT CLAIMED"):strings.Index(got, "INVOICES ALREADY SETTLED")]
	if strings.Contains(claimed, "Invoices claimed        : 3") {
		t.Errorf("a settled invoice was counted in the claim\n---\n%s", claimed)
	}
	if !strings.Contains(claimed, "Invoices claimed        : 2") {
		t.Errorf("expected two invoices in the claim\n---\n%s", claimed)
	}
}

func TestSamadhaanPacketWithNothingOutstanding(t *testing.T) {
	invs := testInvoices(t)
	got := SamadhaanPacket(testSupplier(), "Unrelated Traders", invs[2:3], mustDay(t, "2026-06-15"), BankRateDefaultPct)

	if !mentions(got, "No unpaid invoices for this buyer") {
		t.Errorf("packet should say plainly that there is nothing to claim\n---\n%s", got)
	}
	if !strings.Contains(got, "Total claimed           : \u20b9 0.00") {
		t.Errorf("packet should total a nil claim as zero\n---\n%s", got)
	}
}

func TestSamadhaanPacketWithAnEmptySupplierProfile(t *testing.T) {
	invs := testInvoices(t)
	got := SamadhaanPacket(Supplier{}, "Nandi Agro Equipment Ltd", invs, mustDay(t, "2026-06-15"), BankRateDefaultPct)

	if !strings.Contains(got, "Udyam Registration No.  : —") {
		t.Errorf("a missing Udyam number should show as a visible blank to be filled in\n---\n%s", got)
	}
	if strings.Contains(got, "%!") {
		t.Errorf("packet contains a formatting fault\n---\n%s", got)
	}
}

// The tool prepares documents; it never sends or files anything. Every
// generated document has to say so and must never claim to have done it.
func TestDocumentsNeverClaimToHaveSentOrFiledAnything(t *testing.T) {
	sup := testSupplier()
	invs := testInvoices(t)
	asOf := mustDay(t, "2026-06-15")

	docs := map[string]string{
		"reminder": ReminderLetter(sup, invs[0], asOf, BankRateDefaultPct),
		"notice":   FormalNotice(sup, invs[0], asOf, BankRateDefaultPct),
		"packet":   SamadhaanPacket(sup, "Nandi Agro Equipment Ltd", invs, asOf, BankRateDefaultPct),
	}
	forbidden := []string{
		"we have sent", "has been emailed", "we have filed", "has been filed with",
		"complaint filed", "application submitted", "sent by email on your behalf",
	}
	for name, doc := range docs {
		low := strings.ToLower(doc)
		for _, f := range forbidden {
			if mentions(low, f) {
				t.Errorf("%s claims an action the tool never takes: %q", name, f)
			}
		}
		if !mentions(doc, "Nothing has been sent, emailed or filed") {
			t.Errorf("%s is missing the line saying nothing was sent\n---\n%s", name, doc)
		}
	}
}

func TestGeneratedDocumentLinesFitOnPaper(t *testing.T) {
	sup := testSupplier()
	invs := testInvoices(t)
	asOf := mustDay(t, "2026-06-15")

	// Prose must wrap inside lineWidth; the Samadhaan schedule is a wide
	// table and says so itself, so only the letters are checked here.
	docs := map[string]string{
		"reminder": ReminderLetter(sup, invs[1], asOf, BankRateDefaultPct),
		"notice":   FormalNotice(sup, invs[1], asOf, BankRateDefaultPct),
	}
	for name, doc := range docs {
		for i, line := range strings.Split(doc, "\n") {
			if n := utf8.RuneCountInString(line); n > lineWidth {
				t.Errorf("%s line %d is %d characters, over the %d-character limit:\n%s",
					name, i+1, n, lineWidth, line)
			}
		}
	}
}

func TestWrapIndent(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		width int
		first string
		rest  string
		want  string
	}{
		{name: "short text is untouched", text: "one two", width: 20, want: "one two"},
		{name: "wraps on the word boundary", text: "aaa bbb ccc ddd", width: 7, want: "aaa bbb\nccc ddd"},
		{name: "collapses runs of whitespace", text: "aaa   bbb\nccc", width: 40, want: "aaa bbb ccc"},
		{name: "empty text gives an empty line", text: "   ", width: 20, want: ""},
		{
			name: "hanging indent on a numbered item",
			text: "aaa bbb ccc ddd", width: 10, first: "1. ", rest: "   ",
			want: "1. aaa bbb\n   ccc ddd",
		},
		{
			name: "a word longer than the width is not broken",
			text: "aaaaaaaaaaaa bb", width: 5,
			want: "aaaaaaaaaaaa\nbb",
		},
		{name: "empty text keeps no trailing prefix spaces", text: "", width: 10, first: "1. ", want: "1."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := wrapIndent(tt.text, tt.width, tt.first, tt.rest); got != tt.want {
				t.Fatalf("wrapIndent(%q, %d, %q, %q) =\n%q\nwant\n%q", tt.text, tt.width, tt.first, tt.rest, got, tt.want)
			}
		})
	}
}

func TestRenderTableAligns(t *testing.T) {
	cols := []column{{head: "Item"}, {head: "Amount", right: true}}
	rows := [][]string{
		{"short", "1.00"},
		{"a much longer item", "1,00,000.00"},
	}
	got := renderTable(cols, rows)
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected a head, a rule and two rows, got %d lines:\n%s", len(lines), got)
	}
	width := utf8.RuneCountInString(lines[0])
	for i, l := range lines {
		if utf8.RuneCountInString(l) != width && i != 0 {
			// Trailing spaces are trimmed, so only full rows must match.
			if !strings.HasSuffix(l, "1.00") && !strings.HasSuffix(l, "0.00") && !strings.HasPrefix(l, "-") {
				t.Fatalf("line %d is %d wide, want %d:\n%s", i, utf8.RuneCountInString(l), width, got)
			}
		}
	}
	if !strings.Contains(lines[3], "  1,00,000.00") {
		t.Fatalf("money column should be right aligned:\n%s", got)
	}
	if !strings.HasPrefix(lines[1], "----") {
		t.Fatalf("expected a rule under the headings:\n%s", got)
	}
}

func TestFlattenAddress(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{in: "Line one\nLine two", want: "Line one, Line two"},
		{in: "  Line one  \n\n  Line two \n", want: "Line one, Line two"},
		{in: "", want: ""},
		{in: "Single line", want: "Single line"},
	}
	for _, tt := range tests {
		if got := flatten(tt.in); got != tt.want {
			t.Fatalf("flatten(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
