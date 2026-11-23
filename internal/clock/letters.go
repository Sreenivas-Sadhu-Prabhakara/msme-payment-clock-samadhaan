package clock

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// NoticePayDays is the period a formal notice allows for payment before the
// supplier considers a reference to the Facilitation Council. It is a drafting
// convention, not a statutory period.
const NoticePayDays = 15

// lineWidth is the column the generated documents wrap at, so a print-out
// lands inside the margins of A4 paper.
const lineWidth = 78

// rupee keeps the symbol in one place; the letters are plain text, so the
// amount is spelled out beside it in the Indian digit grouping.
const rupee = "₹ "

// draftFooter closes every generated document. The tool prepares paper; the
// supplier sends it. Nothing here is transmitted, emailed or filed anywhere.
const draftFooter = `------------------------------------------------------------------------------
Prepared on your own machine. Nothing has been sent, emailed or filed. Review
the wording, put it on your letterhead, sign it, and issue it yourself — and
keep proof of delivery, which a Facilitation Council will ask for.`

// packetFooter closes the Samadhaan working paper.
const packetFooter = `------------------------------------------------------------------------------
Prepared on your own machine. Nothing has been sent, emailed or filed. A
reference is made by the supplier, in person, on the MSME Samadhaan portal of
the Ministry of Micro, Small and Medium Enterprises. Check every figure above
against your own books and your buyer's ledger before you rely on it.`

// ReminderLetter drafts the first, polite request for payment: a plain
// statement of the invoice, the date it fell due and what is outstanding, with
// no demand and no threat.
func ReminderLetter(sup Supplier, inv Invoice, asOf Day, bankRatePct float64) string {
	st := inv.Status(asOf, bankRatePct)
	var b strings.Builder

	writeLetterhead(&b, sup, asOf)
	writeAddressee(&b, inv)

	fmt.Fprintf(&b, "Subject: Invoice %s dated %s — request for payment\n\n", inv.No, inv.Date.Pretty())
	b.WriteString("Dear Sir / Madam,\n\n")

	para(&b, fmt.Sprintf(
		"We refer to our invoice %s dated %s for %s%s, against goods supplied or services rendered to you and accepted on %s.",
		inv.No, inv.Date.Pretty(), rupee, FormatINR(inv.Amount), inv.AcceptanceDate.Pretty()))

	switch st.State {
	case Settled:
		para(&b, fmt.Sprintf(
			"Our records show this invoice as settled in full on %s. If that does not match your ledger, please let us know and we will reconcile it.",
			inv.ReceivedDate.Pretty()))
	case Within:
		para(&b, fmt.Sprintf(
			"The amount falls due on %s, %d day(s) from today. This note is only to keep the invoice on your radar ahead of that date.",
			st.AppointedDay.Pretty(), st.DaysLeft))
	default:
		para(&b, fmt.Sprintf(
			"The amount fell due on %s. As on %s, %s%s is still outstanding — %d day(s) past that date.",
			st.AppointedDay.Pretty(), asOf.Pretty(), rupee, FormatINR(st.OutstandingPrincipal), st.DaysOverdue))
		if inv.AmountReceived > 0 {
			para(&b, fmt.Sprintf("We have received %s%s against this invoice, on %s, and are grateful for it.",
				rupee, FormatINR(inv.AmountReceived), inv.ReceivedDate.Pretty()))
		}
	}

	para(&b, "We are a micro / small enterprise and our working capital turns on invoices being settled on time, so we would be grateful if you could release payment at your earliest convenience. If payment has already been made, kindly share the UTR or cheque details so that we can close it in our books.")

	if st.State == Overdue {
		para(&b, "We would far rather settle this with a simple payment than invoke the interest provisions of the Micro, Small and Medium Enterprises Development Act 2006.")
	}

	para(&b, "Thank you for your business.")
	writeSignOff(&b, sup)
	b.WriteString("\n" + draftFooter + "\n")
	return b.String()
}

// FormalNotice drafts the escalation: a notice of demand that states the
// appointed day under s.15, the statutory interest under s.16 with its
// working, the total claimed, and what the supplier will do next.
func FormalNotice(sup Supplier, inv Invoice, asOf Day, bankRatePct float64) string {
	st := inv.Status(asOf, bankRatePct)
	var b strings.Builder

	writeLetterhead(&b, sup, asOf)
	writeAddressee(&b, inv)

	b.WriteString(wrapIndent(fmt.Sprintf(
		"Notice of demand — invoice %s, payment and interest due under sections 15 and 16 of the MSMED Act 2006", inv.No),
		lineWidth, "Subject: ", "         ") + "\n\n")
	b.WriteString("Dear Sir / Madam,\n\n")
	para(&b, "We place the following on record.")

	n := 0
	item := func(text string) {
		n++
		b.WriteString(wrapIndent(text, lineWidth, fmt.Sprintf("%d. ", n), "   ") + "\n\n")
	}

	item(fmt.Sprintf(
		"Our invoice %s dated %s was raised for %s%s against goods supplied or services rendered to you and accepted on %s.",
		inv.No, inv.Date.Pretty(), rupee, FormatINR(inv.Amount), inv.AcceptanceDate.Pretty()))

	if st.AgreedInWriting {
		text := fmt.Sprintf(
			"The payment date agreed in writing was %s. Section 15 of the Micro, Small and Medium Enterprises Development Act 2006 provides that a period agreed in writing may in no case exceed forty-five days from the day of acceptance, so the appointed day for this supply is %s.",
			inv.AgreedPaymentDate.Pretty(), st.AppointedDay.Pretty())
		if st.CappedAt45Days {
			text += fmt.Sprintf(" The agreed date falls beyond that statutory maximum and is therefore read down to %d days from acceptance.", MaxAgreedDays)
		}
		item(text)
	} else {
		item(fmt.Sprintf(
			"There is no agreement in writing on the period for payment. Under section 15 of the Micro, Small and Medium Enterprises Development Act 2006 the period is therefore fifteen days from the day of acceptance, and the appointed day for this supply is %s.",
			st.AppointedDay.Pretty()))
	}

	item(fmt.Sprintf("As on %s, %s%s of principal remains unpaid, %d day(s) past the appointed day.",
		asOf.Pretty(), rupee, FormatINR(st.OutstandingPrincipal), st.DaysOverdue))

	n++
	b.WriteString(wrapIndent(fmt.Sprintf(
		"Section 16 of the Act makes you liable to pay compound interest, with monthly rests, at three times the Bank Rate notified by the Reserve Bank of India, from the appointed day. At a Bank Rate of %.2f%% per annum that is %.2f%% per annum. Computed from %s to %s the interest is %s%s, worked out as follows:",
		bankRatePct, AnnualRatePct(bankRatePct), st.AppointedDay.Pretty(), asOf.Pretty(), rupee, FormatINR(st.Interest)),
		lineWidth, fmt.Sprintf("%d. ", n), "   ") + "\n\n")
	b.WriteString(indentBlock(workingNote(st), "   "))
	b.WriteString("\n")

	item(fmt.Sprintf("The amount now payable is %s%s of principal together with %s%s of interest, in all %s%s.",
		rupee, FormatINR(st.OutstandingPrincipal), rupee, FormatINR(st.Interest), rupee, FormatINR(st.TotalDue)))

	item(fmt.Sprintf("We call upon you to pay that amount within %d days of receipt of this notice. Interest continues to accrue until payment is actually received.", NoticePayDays))

	item("Should the amount remain unpaid, we may refer the dispute to the Micro and Small Enterprises Facilitation Council under section 18 of the Act, through the MSME Samadhaan portal, without further notice. Your attention is also drawn to section 43B(h) of the Income-tax Act 1961, under which a sum payable to a micro or small enterprise beyond the time limit in section 15 of the MSMED Act is allowed as a deduction only in the previous year in which it is actually paid.")

	para(&b, "This notice is issued without prejudice to our other rights and remedies.")
	writeSignOff(&b, sup)
	b.WriteString("\n" + draftFooter + "\n")
	return b.String()
}

// SamadhaanPacket assembles everything a reference to the Micro and Small
// Enterprises Facilitation Council needs against one buyer: the supplier's
// Udyam registration, the buyer's details, the schedule of invoices, the
// principal, the computed interest and the basis of the computation — on one
// printable page the supplier can carry to the MSME Samadhaan portal.
//
// Only the named buyer's invoices are included; a reference lies against one
// buyer at a time. Invoices settled in full are listed separately, so the
// Council can see the whole dealing, and their late-payment interest is shown
// but not claimed as principal.
func SamadhaanPacket(sup Supplier, buyer string, invoices []Invoice, asOf Day, bankRatePct float64) string {
	mine := ForBuyer(invoices, buyer)
	var b strings.Builder

	n := 0
	section := func(title string) {
		n++
		fmt.Fprintf(&b, "%d. %s\n\n", n, title)
	}

	b.WriteString("MSME SAMADHAAN — WORKING PAPER FOR A REFERENCE UNDER SECTION 18\nMicro, Small and Medium Enterprises Development Act 2006\n")
	fmt.Fprintf(&b, "Prepared on %s\n\n", asOf.Pretty())

	section("APPLICANT (SUPPLIER)")
	writeField(&b, "Name", sup.Name)
	writeField(&b, "Udyam Registration No.", NormaliseUdyam(sup.UdyamNumber))
	writeField(&b, "Address", flatten(sup.Address))
	writeField(&b, "Contact", sup.Contact)
	writeField(&b, "Email", sup.Email)

	b.WriteString("\n")
	section("RESPONDENT (BUYER)")
	writeField(&b, "Name", strings.TrimSpace(buyer))
	writeField(&b, "GSTIN", firstGSTIN(mine))

	unpaid, settled := split(mine, asOf, bankRatePct)

	b.WriteString("\n")
	section("SCHEDULE OF INVOICES CLAIMED")
	if len(unpaid) == 0 {
		b.WriteString("   No unpaid invoices for this buyer as on the date above.\n")
	} else {
		b.WriteString(indentBlock(scheduleTable(unpaid, asOf, bankRatePct), "   "))
	}

	sum := Totals(unpaid, asOf, bankRatePct)
	b.WriteString("\n")
	section("AMOUNT CLAIMED")
	writeField(&b, "Invoices claimed", fmt.Sprintf("%d", len(unpaid)))
	writeField(&b, "Principal outstanding", rupee+FormatINR(sum.OutstandingPrincipal))
	writeField(&b, "Interest under s.16", rupee+FormatINR(sum.Interest))
	writeField(&b, "Total claimed", rupee+FormatINR(sum.TotalDue))

	if len(settled) > 0 {
		b.WriteString("\n")
		section("INVOICES ALREADY SETTLED (shown for completeness, not claimed)")
		b.WriteString(indentBlock(scheduleTable(settled, asOf, bankRatePct), "   "))
		late := Totals(settled, asOf, bankRatePct)
		fmt.Fprintf(&b, "\n   Interest on invoices settled after the appointed day: %s%s.\n",
			rupee, FormatINR(late.Interest))
	}

	b.WriteString("\n")
	section("BASIS OF COMPUTATION")
	b.WriteString(indentBlock(basisNote(bankRatePct), "   "))

	b.WriteString("\n")
	section("DOCUMENTS TO ATTACH")
	b.WriteString("   [ ] Udyam Registration Certificate of the supplier\n")
	b.WriteString("   [ ] Copy of each invoice listed above\n")
	b.WriteString("   [ ] Purchase order, work order or written agreement, if any\n")
	b.WriteString("   [ ] Proof of supply and of acceptance — delivery challan, e-way bill,\n       goods receipt note, signed acceptance or completion certificate\n")
	b.WriteString("   [ ] Ledger or bank statement showing what has been received\n")
	b.WriteString("   [ ] The reminder and the notice of demand, with proof of delivery\n")

	b.WriteString("\n" + packetFooter + "\n")
	return b.String()
}

// scheduleTable renders the invoice schedule as a fixed-width table.
func scheduleTable(invoices []Invoice, asOf Day, bankRatePct float64) string {
	cols := []column{
		{head: "Invoice"},
		{head: "Inv. date"},
		{head: "Accepted"},
		{head: "Appointed"},
		{head: "Amount", right: true},
		{head: "Received", right: true},
		{head: "Unpaid", right: true},
		{head: "Days", right: true},
		{head: "Interest", right: true},
	}
	rows := make([][]string, 0, len(invoices))
	for _, inv := range invoices {
		st := inv.Status(asOf, bankRatePct)
		rows = append(rows, []string{
			inv.No,
			inv.Date.String(),
			inv.AcceptanceDate.String(),
			st.AppointedDay.String(),
			FormatINR(inv.Amount),
			FormatINR(inv.AmountReceived),
			FormatINR(st.OutstandingPrincipal),
			fmt.Sprintf("%d", st.DaysOverdue),
			FormatINR(st.Interest),
		})
	}
	return renderTable(cols, rows) +
		"All amounts in rupees. Days = days past the appointed day.\n" +
		"The schedule is wide: print this page in landscape so no column is cut off.\n"
}

// workingNote shows how one interest figure was built up.
func workingNote(st Status) string {
	p := st.Period
	var b strings.Builder
	row := func(label, value string) { fmt.Fprintf(&b, "%-14s : %s\n", label, value) }
	row("Principal", rupee+FormatINR(p.Principal))
	row("Period", fmt.Sprintf("%s to %s (%d days)", p.From.Pretty(), p.To.Pretty(), p.Days))
	row("Rate", fmt.Sprintf("%.2f%% per annum (%d x Bank Rate of %.2f%%)", p.AnnualRatePct, InterestMultiple, p.BankRatePct))
	row("Monthly rests", fmt.Sprintf("%d completed month(s), compounded", p.CompletedMonths))
	row("Part month", fmt.Sprintf("%d day(s), simple, on a %d-day year", p.PartMonthDays, DaysInYear))
	row("Interest", rupee+FormatINR(st.Interest))
	return b.String()
}

// basisNote states the rules and the figures the computation rests on.
func basisNote(bankRatePct float64) string {
	paras := []string{
		fmt.Sprintf("Appointed day (s.15): the date agreed in writing, or %d days from the day of acceptance or deemed acceptance where there is no agreement in writing. A period agreed in writing may in no case exceed %d days from acceptance, so any longer agreed date is read down to %d days.",
			NoAgreementDays, MaxAgreedDays, MaxAgreedDays),
		fmt.Sprintf("Interest (s.16): compound interest with monthly rests at three times the Bank Rate notified by the Reserve Bank of India, from the appointed day. The Bank Rate is taken as %.2f%% per annum, giving %.2f%% per annum. Interest for each completed month from the appointed day is added to the balance and itself carries interest; the trailing part month is charged as simple interest on a %d-day year.",
			bankRatePct, AnnualRatePct(bankRatePct), DaysInYear),
		"The Bank Rate changes. Confirm the rate that applied over this period against the Reserve Bank of India's own notification and re-run the figures if it differs.",
		"The appointed day shown is the last day of the statutory period. Section 2(b) of the Act defines the appointed day as the day immediately following the expiry of that period, so interest here runs from the day after the date shown.",
		"Where an invoice has been part paid, interest is computed on the principal still outstanding from the appointed day, which understates rather than overstates the claim.",
		"Only a micro or small enterprise registered on the Udyam portal can rely on these provisions. A medium enterprise cannot.",
	}
	for i, p := range paras {
		paras[i] = wrap(p, lineWidth-3)
	}
	return strings.Join(paras, "\n\n") + "\n"
}

func split(invoices []Invoice, asOf Day, bankRatePct float64) (unpaid, settled []Invoice) {
	for _, inv := range invoices {
		if inv.Status(asOf, bankRatePct).State == Settled {
			settled = append(settled, inv)
			continue
		}
		unpaid = append(unpaid, inv)
	}
	return unpaid, settled
}

func firstGSTIN(invoices []Invoice) string {
	for _, inv := range invoices {
		if g := NormaliseGSTIN(inv.BuyerGSTIN); g != "" {
			return g
		}
	}
	return ""
}

func writeLetterhead(b *strings.Builder, sup Supplier, asOf Day) {
	name := strings.TrimSpace(sup.Name)
	if name == "" {
		name = "[Your firm's name]"
	}
	b.WriteString(name + "\n")
	if addr := strings.TrimSpace(sup.Address); addr != "" {
		b.WriteString(addr + "\n")
	}
	if u := NormaliseUdyam(sup.UdyamNumber); u != "" {
		b.WriteString("Udyam Registration No.: " + u + "\n")
	}
	var reach []string
	if c := strings.TrimSpace(sup.Contact); c != "" {
		reach = append(reach, c)
	}
	if e := strings.TrimSpace(sup.Email); e != "" {
		reach = append(reach, e)
	}
	if len(reach) > 0 {
		b.WriteString(strings.Join(reach, " · ") + "\n")
	}
	fmt.Fprintf(b, "\nDate: %s\n\n", asOf.Pretty())
}

func writeAddressee(b *strings.Builder, inv Invoice) {
	b.WriteString("To\n")
	buyer := strings.TrimSpace(inv.BuyerName)
	if buyer == "" {
		buyer = "[Buyer's name]"
	}
	b.WriteString(buyer + "\n")
	if g := NormaliseGSTIN(inv.BuyerGSTIN); g != "" {
		b.WriteString("GSTIN: " + g + "\n")
	}
	b.WriteString("\n")
}

func writeSignOff(b *strings.Builder, sup Supplier) {
	b.WriteString("Yours faithfully,\n\n\n")
	if name := strings.TrimSpace(sup.Name); name != "" {
		b.WriteString("For " + name + "\n")
	} else {
		b.WriteString("For [your firm's name]\n")
	}
	b.WriteString("Authorised signatory\n")
}

func writeField(b *strings.Builder, label, value string) {
	if strings.TrimSpace(value) == "" {
		value = "—"
	}
	fmt.Fprintf(b, "   %-24s: %s\n", label, value)
}

// para writes one wrapped paragraph followed by a blank line.
func para(b *strings.Builder, text string) {
	b.WriteString(wrap(text, lineWidth) + "\n\n")
}

// wrap breaks text into lines of at most width characters.
func wrap(text string, width int) string {
	return wrapIndent(text, width, "", "")
}

// wrapIndent wraps text to width, prefixing the first line with first and
// every later line with rest, which is how a numbered item keeps its hanging
// indent. Whitespace in the input, newlines included, is treated as a single
// space between words.
func wrapIndent(text string, width int, first, rest string) string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return strings.TrimRight(first, " ")
	}
	var lines []string
	line := first
	length := utf8.RuneCountInString(first)
	for i, w := range words {
		wl := utf8.RuneCountInString(w)
		if i > 0 && length+1+wl > width {
			lines = append(lines, line)
			line = rest + w
			length = utf8.RuneCountInString(rest) + wl
			continue
		}
		if i > 0 {
			line += " " + w
			length += 1 + wl
			continue
		}
		line += w
		length += wl
	}
	return strings.Join(append(lines, line), "\n")
}

func flatten(s string) string {
	var out []string
	for _, part := range strings.Split(s, "\n") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, ", ")
}

func indentBlock(s, pad string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		if l == "" {
			continue
		}
		lines[i] = pad + l
	}
	return strings.Join(lines, "\n") + "\n"
}

// column describes one column of a fixed-width table.
type column struct {
	head  string
	right bool
}

// renderTable lays out rows in columns padded to the widest cell, so the
// schedule stays aligned in a monospaced print-out.
func renderTable(cols []column, rows [][]string) string {
	width := make([]int, len(cols))
	for i, c := range cols {
		width[i] = utf8.RuneCountInString(c.head)
	}
	for _, r := range rows {
		for i, cell := range r {
			if i < len(width) && utf8.RuneCountInString(cell) > width[i] {
				width[i] = utf8.RuneCountInString(cell)
			}
		}
	}
	var b strings.Builder
	line := func(cells []string) {
		parts := make([]string, 0, len(cells))
		for i, cell := range cells {
			if i >= len(width) {
				continue
			}
			parts = append(parts, pad(cell, width[i], cols[i].right))
		}
		b.WriteString(strings.TrimRight(strings.Join(parts, "  "), " ") + "\n")
	}
	heads := make([]string, len(cols))
	rules := make([]string, len(cols))
	for i, c := range cols {
		heads[i] = c.head
		rules[i] = strings.Repeat("-", width[i])
	}
	line(heads)
	line(rules)
	for _, r := range rows {
		line(r)
	}
	return b.String()
}

func pad(s string, w int, right bool) string {
	gap := w - utf8.RuneCountInString(s)
	if gap <= 0 {
		return s
	}
	if right {
		return strings.Repeat(" ", gap) + s
	}
	return s + strings.Repeat(" ", gap)
}
