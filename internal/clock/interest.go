package clock

import (
	"math"
	"strconv"
	"strings"
)

// Statutory constants. Every figure the computation depends on is named here,
// once, so it can be checked against the Act and against the current rate
// published by the Reserve Bank of India.
const (
	// BankRateDefaultPct is the default Reserve Bank of India Bank Rate, in
	// percent per year, used when the user has not entered one.
	//
	// The Bank Rate CHANGES whenever the RBI revises it, and the statutory
	// interest under s.16 is three times whatever the Bank Rate is for the
	// period concerned. Treat this only as a starting value: confirm the
	// current Bank Rate with the RBI and override it in the tool.
	BankRateDefaultPct = 6.50

	// InterestMultiple is the statutory multiple of the Bank Rate at which
	// interest is payable under s.16 of the MSMED Act 2006 — "three times of
	// the bank rate notified by the Reserve Bank".
	InterestMultiple = 3

	// MaxAgreedDays is the outer limit, in days from the day of acceptance or
	// deemed acceptance, that a written agreement may allow for payment under
	// s.15 of the MSMED Act 2006 ("in no case shall the period agreed upon
	// between the supplier and the buyer in writing exceed forty-five days").
	MaxAgreedDays = 45

	// NoAgreementDays is the period allowed under s.15 when there is no
	// agreement in writing: fifteen days from the day of acceptance or deemed
	// acceptance.
	NoAgreementDays = 15

	// DaysInYear is the day-count basis used to pro-rate interest for the
	// trailing part-month after the last monthly rest.
	DaysInYear = 365

	// MonthsInYear converts the annual statutory rate into the rate applied at
	// each monthly rest.
	MonthsInYear = 12
)

// AnnualRatePct returns the statutory annual interest rate, in percent, for a
// given Bank Rate: three times the Bank Rate (s.16, MSMED Act 2006).
func AnnualRatePct(bankRatePct float64) float64 {
	return bankRatePct * InterestMultiple
}

// Interest returns the statutory interest that has accrued on principal between
// two dates under s.16 of the MSMED Act 2006: compound interest with monthly
// rests at three times the Reserve Bank of India Bank Rate.
//
// "Monthly rests" is implemented literally. Interest for each completed
// calendar month from the start date is added to the balance and itself earns
// interest from then on, at one twelfth of the annual rate. Whatever is left
// after the last complete month — the trailing part-month — is charged as
// simple interest on that balance, pro-rated over a 365-day year.
//
// Dates are inclusive of the start and exclusive of the end, so an unpaid
// invoice carries no interest on the appointed day itself. The result is not
// rounded; round once, at the point where the figure is shown or printed.
func Interest(principal float64, from, to Day, bankRatePct float64) float64 {
	interest, _, _ := accrue(principal, from, to, bankRatePct)
	return interest
}

// accrue is the single implementation of the s.16 computation: it returns the
// interest together with the shape of the period, so the figure shown to the
// user and the working printed beside it can never drift apart.
//
// Each monthly rest is measured from the start date itself rather than from the
// previous rest, so a clamped short month does not drag every later rest
// backwards: from 31 January the rests fall on 29 February, 31 March, 30 April.
func accrue(principal float64, from, to Day, bankRatePct float64) (interest float64, months, partDays int) {
	if principal <= 0 || bankRatePct <= 0 || from.IsZero() || to.IsZero() || !to.After(from) {
		return 0, 0, 0
	}
	annual := AnnualRatePct(bankRatePct) / 100
	balance := principal
	for !from.AddMonths(months + 1).After(to) {
		balance += balance * annual / MonthsInYear
		months++
	}
	if days := from.AddMonths(months).DaysUntil(to); days > 0 {
		balance += balance * annual * float64(days) / DaysInYear
		partDays = days
	}
	return balance - principal, months, partDays
}

// InterestPeriod describes how an interest figure was arrived at, so a printed
// document can show the working rather than just the number.
type InterestPeriod struct {
	From            Day     `json:"from"`
	To              Day     `json:"to"`
	Days            int     `json:"days"`
	CompletedMonths int     `json:"completedMonths"`
	PartMonthDays   int     `json:"partMonthDays"`
	BankRatePct     float64 `json:"bankRatePct"`
	AnnualRatePct   float64 `json:"annualRatePct"`
	Principal       float64 `json:"principal"`
	Interest        float64 `json:"interest"`
}

// Describe computes the interest and the shape of the period it accrued over.
func Describe(principal float64, from, to Day, bankRatePct float64) InterestPeriod {
	interest, months, partDays := accrue(principal, from, to, bankRatePct)
	p := InterestPeriod{
		From:            from,
		To:              to,
		CompletedMonths: months,
		PartMonthDays:   partDays,
		BankRatePct:     bankRatePct,
		AnnualRatePct:   AnnualRatePct(bankRatePct),
		Principal:       principal,
		Interest:        interest,
	}
	if !from.IsZero() && !to.IsZero() && to.After(from) {
		p.Days = from.DaysUntil(to)
	}
	return p
}

// Round2 rounds a money amount to two decimal places (paise).
func Round2(v float64) float64 {
	return math.Round(v*100) / 100
}

// FormatINR renders a rupee amount with two decimals in the Indian digit
// grouping used on invoices and in filings: 12,34,567.89 rather than
// 1,234,567.89. The symbol is left to the caller so plain-text letters stay
// readable in any font.
func FormatINR(v float64) string {
	v = Round2(v)
	if v == 0 {
		return "0.00" // also catches a negative zero from rounding
	}
	neg := v < 0
	if neg {
		v = -v
	}
	s := strconv.FormatFloat(v, 'f', 2, 64)
	whole, frac := s[:len(s)-3], s[len(s)-3:]
	out := groupIndian(whole) + frac
	if neg {
		return "-" + out
	}
	return out
}

// groupIndian inserts separators after the last three digits and every two
// digits before that (thousand, lakh, crore).
func groupIndian(whole string) string {
	if len(whole) <= 3 {
		return whole
	}
	head, tail := whole[:len(whole)-3], whole[len(whole)-3:]
	var parts []string
	for len(head) > 2 {
		parts = append([]string{head[len(head)-2:]}, parts...)
		head = head[:len(head)-2]
	}
	if head != "" {
		parts = append([]string{head}, parts...)
	}
	return strings.Join(parts, ",") + "," + tail
}
