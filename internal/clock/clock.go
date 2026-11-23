// Package clock implements the payment clock of the Micro, Small and Medium
// Enterprises Development Act 2006 for a registered micro or small supplier.
//
// Two rules carry the whole package. Under s.15 a buyer must pay the supplier
// on or before the date agreed in writing, and that agreed period may never
// exceed forty-five days from the day of acceptance or deemed acceptance; with
// no agreement in writing the period is fifteen days. Under s.16, once that
// appointed day passes, the buyer is liable to pay compound interest with
// monthly rests at three times the Bank Rate notified by the Reserve Bank of
// India, from the appointed day.
//
// Everything here is a pure function over plain values: no clock reads, no
// file access, no network. The caller supplies the "as of" date, which is what
// makes the whole computation reproducible and testable.
package clock

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// moneyEpsilon is half a paise: amounts closer than this are treated as equal,
// so a fully-settled invoice is not left looking a fraction short.
const moneyEpsilon = 0.005

// Supplier is the micro or small enterprise chasing payment. These details go
// at the head of every letter and of the Samadhaan working paper.
type Supplier struct {
	Name        string `json:"name"`
	UdyamNumber string `json:"udyamNumber"`
	Address     string `json:"address"`
	Contact     string `json:"contact"`
	Email       string `json:"email"`
}

// Invoice is one supply to one buyer, with whatever has been received against
// it so far.
//
// AcceptanceDate is the day of acceptance or deemed acceptance of the goods or
// services — the day the statutory clock starts, which is not necessarily the
// invoice date. AgreedPaymentDate is optional: leave it unset when there is no
// agreement in writing.
type Invoice struct {
	ID                string  `json:"id"`
	No                string  `json:"no"`
	Date              Day     `json:"date"`
	AcceptanceDate    Day     `json:"acceptanceDate"`
	Amount            float64 `json:"amount"`
	BuyerName         string  `json:"buyerName"`
	BuyerGSTIN        string  `json:"buyerGstin"`
	AgreedPaymentDate Day     `json:"agreedPaymentDate"`
	AmountReceived    float64 `json:"amountReceived"`
	ReceivedDate      Day     `json:"receivedDate"`
}

// State is where an invoice stands against its appointed day.
type State string

const (
	// Within means the appointed day has not passed: no interest is due yet.
	Within State = "within"
	// Overdue means the appointed day has passed with money still outstanding.
	Overdue State = "overdue"
	// Settled means the full invoice amount has been received.
	Settled State = "settled"
)

// Status is the computed position of one invoice on a given date.
type Status struct {
	State State `json:"state"`
	// AppointedDay is the last day the buyer may pay without attracting
	// statutory interest.
	AppointedDay Day `json:"appointedDay"`
	// AgreedInWriting records which limb of s.15 set the appointed day.
	AgreedInWriting bool `json:"agreedInWriting"`
	// CappedAt45Days is true when a written agreement allowed longer than the
	// statutory maximum and was cut back to forty-five days.
	CappedAt45Days bool `json:"cappedAt45Days"`
	// DaysLeft counts down to the appointed day while the invoice is Within.
	DaysLeft int `json:"daysLeft"`
	// DaysOverdue counts days past the appointed day. For a Settled invoice it
	// is how late the payment was.
	DaysOverdue          int            `json:"daysOverdue"`
	OutstandingPrincipal float64        `json:"outstandingPrincipal"`
	Interest             float64        `json:"interest"`
	TotalDue             float64        `json:"totalDue"`
	Period               InterestPeriod `json:"period"`
}

// AppointedDay returns the last day on which the buyer may pay without
// attracting interest under s.16, given the day of acceptance and the payment
// date agreed in writing.
//
//   - With an agreed date: the earlier of that date and forty-five days from
//     acceptance, because s.15 caps any agreed period at forty-five days.
//   - With no agreed date (the zero Day): fifteen days from acceptance.
//
// The forty-five day cap applies in every case. An unset acceptance date
// yields the zero Day — the clock cannot start until the supply is accepted.
//
// s.2(b) of the Act phrases the appointed day as the day immediately following
// the expiry of that period; this function returns the last day of the period
// itself, so interest begins the day after the date returned here.
func AppointedDay(acceptanceDate, agreedDate Day) Day {
	if acceptanceDate.IsZero() {
		return Day{}
	}
	limit := acceptanceDate.AddDays(MaxAgreedDays)
	if agreedDate.IsZero() {
		return acceptanceDate.AddDays(NoAgreementDays)
	}
	if agreedDate.After(limit) {
		return limit
	}
	return agreedDate
}

// AppointedDay returns the invoice's appointed day under s.15.
func (inv Invoice) AppointedDay() Day {
	return AppointedDay(inv.AcceptanceDate, inv.AgreedPaymentDate)
}

// DaysOverdue counts the days that have passed since the appointed day as at
// asOf. It is zero while the invoice is still within time, and zero on the
// appointed day itself, which is still a day for timely payment.
func (inv Invoice) DaysOverdue(asOf Day) int {
	appointed := inv.AppointedDay()
	if appointed.IsZero() || asOf.IsZero() {
		return 0
	}
	if days := appointed.DaysUntil(asOf); days > 0 {
		return days
	}
	return 0
}

// OutstandingPrincipal is the invoice amount less whatever has been received,
// never negative.
func (inv Invoice) OutstandingPrincipal() float64 {
	out := Round2(inv.Amount - inv.AmountReceived)
	if out < moneyEpsilon {
		return 0
	}
	return out
}

// Status computes where the invoice stands on the given date at the given Bank
// Rate.
//
// A settled invoice that was paid after its appointed day still shows the
// interest that accrued up to the day the money arrived, because paying the
// principal late does not extinguish the s.16 liability.
func (inv Invoice) Status(asOf Day, bankRatePct float64) Status {
	appointed := inv.AppointedDay()
	st := Status{
		State:                Within,
		AppointedDay:         appointed,
		AgreedInWriting:      !inv.AgreedPaymentDate.IsZero(),
		OutstandingPrincipal: inv.OutstandingPrincipal(),
	}
	if st.AgreedInWriting && !inv.AcceptanceDate.IsZero() {
		st.CappedAt45Days = inv.AgreedPaymentDate.After(inv.AcceptanceDate.AddDays(MaxAgreedDays))
	}

	switch {
	case st.OutstandingPrincipal == 0 && inv.Amount > 0:
		st.State = Settled
		paidOn := inv.ReceivedDate
		if paidOn.IsZero() {
			paidOn = asOf
		}
		st.DaysOverdue = max(appointed.DaysUntil(paidOn), 0)
		st.Period = Describe(inv.Amount, appointed, paidOn, bankRatePct)
	default:
		days := inv.DaysOverdue(asOf)
		if days > 0 {
			st.State = Overdue
			st.DaysOverdue = days
			st.Period = Describe(st.OutstandingPrincipal, appointed, asOf, bankRatePct)
		} else if !appointed.IsZero() && !asOf.IsZero() {
			st.DaysLeft = max(asOf.DaysUntil(appointed), 0)
		}
	}

	st.Interest = Round2(st.Period.Interest)
	st.TotalDue = Round2(st.OutstandingPrincipal + st.Interest)
	return st
}

// Summary totals a set of invoices as at one date.
type Summary struct {
	Count                int     `json:"count"`
	Within               int     `json:"within"`
	Overdue              int     `json:"overdue"`
	Settled              int     `json:"settled"`
	OutstandingPrincipal float64 `json:"outstandingPrincipal"`
	Interest             float64 `json:"interest"`
	TotalDue             float64 `json:"totalDue"`
	AsOf                 Day     `json:"asOf"`
	BankRatePct          float64 `json:"bankRatePct"`
	AnnualRatePct        float64 `json:"annualRatePct"`
}

// Totals adds up the invoices as at asOf. Interest includes interest on
// invoices that were settled late, which is still payable.
func Totals(invoices []Invoice, asOf Day, bankRatePct float64) Summary {
	sum := Summary{
		Count:         len(invoices),
		AsOf:          asOf,
		BankRatePct:   bankRatePct,
		AnnualRatePct: AnnualRatePct(bankRatePct),
	}
	for _, inv := range invoices {
		st := inv.Status(asOf, bankRatePct)
		switch st.State {
		case Within:
			sum.Within++
		case Overdue:
			sum.Overdue++
		case Settled:
			sum.Settled++
		}
		sum.OutstandingPrincipal += st.OutstandingPrincipal
		sum.Interest += st.Interest
	}
	sum.OutstandingPrincipal = Round2(sum.OutstandingPrincipal)
	sum.Interest = Round2(sum.Interest)
	sum.TotalDue = Round2(sum.OutstandingPrincipal + sum.Interest)
	return sum
}

// Buyers lists the distinct buyer names across the invoices, sorted, so a
// reference can be built against one buyer at a time.
func Buyers(invoices []Invoice) []string {
	seen := map[string]bool{}
	var out []string
	for _, inv := range invoices {
		name := strings.TrimSpace(inv.BuyerName)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// ForBuyer selects the invoices raised on one buyer, matched on the trimmed
// name, ignoring case.
func ForBuyer(invoices []Invoice, buyer string) []Invoice {
	want := strings.ToLower(strings.TrimSpace(buyer))
	var out []Invoice
	for _, inv := range invoices {
		if strings.ToLower(strings.TrimSpace(inv.BuyerName)) == want {
			out = append(out, inv)
		}
	}
	return out
}

var (
	// A GSTIN is two state-code digits, a ten-character PAN, an entity digit,
	// a reserved letter and a check character.
	gstinRe = regexp.MustCompile(`^[0-9]{2}[A-Z]{5}[0-9]{4}[A-Z][0-9A-Z]{3}$`)
	// Udyam registration numbers read UDYAM-XX-00-0000000.
	udyamRe = regexp.MustCompile(`^UDYAM-[A-Z]{2}-[0-9]{2}-[0-9]{7}$`)
)

// NormaliseGSTIN upper-cases and trims a GSTIN.
func NormaliseGSTIN(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }

// ValidateGSTIN checks the shape of a buyer's GSTIN. An empty value is allowed
// — a buyer may not have been given one — but a wrong one would travel into a
// filing, so it is rejected.
func ValidateGSTIN(s string) error {
	s = NormaliseGSTIN(s)
	if s == "" {
		return nil
	}
	if len(s) != 15 {
		return fmt.Errorf("GSTIN must be 15 characters, got %d", len(s))
	}
	if !gstinRe.MatchString(s) {
		return fmt.Errorf("GSTIN %q is not in the form 29ABCDE1234F1Z5", s)
	}
	return nil
}

// NormaliseUdyam upper-cases and trims a Udyam registration number.
func NormaliseUdyam(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }

// ValidateUdyam checks the shape of the supplier's Udyam registration number.
// Only a registered micro or small enterprise can rely on ss.15-16, so the
// number is what a Facilitation Council will look for first.
func ValidateUdyam(s string) error {
	s = NormaliseUdyam(s)
	if s == "" {
		return nil
	}
	if !udyamRe.MatchString(s) {
		return fmt.Errorf("Udyam number %q is not in the form UDYAM-KR-03-0001234", s)
	}
	return nil
}

// Validate checks one invoice well enough that the computation and the printed
// documents cannot be built on nonsense.
func (inv Invoice) Validate() error {
	if strings.TrimSpace(inv.No) == "" {
		return fmt.Errorf("invoice number is required")
	}
	if strings.TrimSpace(inv.BuyerName) == "" {
		return fmt.Errorf("buyer name is required")
	}
	if inv.Amount <= 0 {
		return fmt.Errorf("invoice amount must be more than zero")
	}
	if inv.AcceptanceDate.IsZero() {
		return fmt.Errorf("date of acceptance is required: the statutory clock runs from it")
	}
	if !inv.Date.IsZero() && inv.AcceptanceDate.Before(inv.Date) {
		return fmt.Errorf("date of acceptance cannot fall before the invoice date")
	}
	if !inv.AgreedPaymentDate.IsZero() && inv.AgreedPaymentDate.Before(inv.AcceptanceDate) {
		return fmt.Errorf("agreed payment date cannot fall before the date of acceptance")
	}
	if inv.AmountReceived < 0 {
		return fmt.Errorf("amount received cannot be negative")
	}
	if inv.AmountReceived > inv.Amount+moneyEpsilon {
		return fmt.Errorf("amount received cannot exceed the invoice amount")
	}
	if inv.AmountReceived > 0 && inv.ReceivedDate.IsZero() {
		return fmt.Errorf("enter the date the payment was received")
	}
	if !inv.ReceivedDate.IsZero() && inv.ReceivedDate.Before(inv.AcceptanceDate) {
		return fmt.Errorf("date received cannot fall before the date of acceptance")
	}
	if err := ValidateGSTIN(inv.BuyerGSTIN); err != nil {
		return err
	}
	return nil
}
