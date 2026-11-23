package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sreenivas-sadhu-prabhakara/msme-payment-clock-samadhaan/internal/clock"
)

// testDay is the fixed date the handlers are tested on, so nothing depends on
// when the suite happens to run.
const testDay = "2026-06-15"

func newTestServer(t *testing.T) (http.Handler, *store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "payment-clock-data.json")
	st, err := openStore(path)
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	srv := newServer(st)
	srv.today = func() clock.Day {
		day, err := clock.ParseDay(testDay)
		if err != nil {
			t.Fatalf("ParseDay: %v", err)
		}
		return day
	}
	return srv.routes(), st, path
}

func do(t *testing.T, h http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeState(t *testing.T, rec *httptest.ResponseRecorder) stateResponse {
	t.Helper()
	var out stateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decoding state: %v\n%s", err, rec.Body.String())
	}
	return out
}

const validInvoice = `{"id":"","no":"VPW/2026/041","date":"2026-01-05","acceptanceDate":"2026-01-10",
	"amount":486500,"buyerName":"Nandi Agro Equipment Ltd","buyerGstin":"29abcde1234f1z5",
	"agreedPaymentDate":"","amountReceived":0,"receivedDate":""}`

func TestStateStartsEmptyAtTheDefaultBankRate(t *testing.T) {
	h, _, _ := newTestServer(t)

	rec := do(t, h, "GET", "/api/state", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/state = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	got := decodeState(t, rec)

	if len(got.Invoices) != 0 || got.Summary.Count != 0 {
		t.Fatalf("a fresh ledger holds %d invoices, want none", len(got.Invoices))
	}
	if got.BankRatePct != clock.BankRateDefaultPct {
		t.Fatalf("BankRatePct = %.2f, want the documented default %.2f", got.BankRatePct, clock.BankRateDefaultPct)
	}
	if got.AnnualRatePct != clock.AnnualRatePct(clock.BankRateDefaultPct) {
		t.Fatalf("AnnualRatePct = %.2f, want %.2f", got.AnnualRatePct, clock.AnnualRatePct(clock.BankRateDefaultPct))
	}
	if got.AsOf.String() != testDay || got.Today.String() != testDay {
		t.Fatalf("as-of/today = %q/%q, want %q", got.AsOf, got.Today, testDay)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("API responses should not be cached, got %q", rec.Header().Get("Cache-Control"))
	}
}

func TestCreateInvoiceComputesTheClockAndPersists(t *testing.T) {
	h, _, path := newTestServer(t)

	rec := do(t, h, "POST", "/api/invoices", validInvoice)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/invoices = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	got := decodeState(t, rec)
	if len(got.Invoices) != 1 {
		t.Fatalf("got %d invoices, want 1", len(got.Invoices))
	}

	view := got.Invoices[0]
	if view.ID == "" {
		t.Fatal("the stored invoice has no record key")
	}
	if view.BuyerGSTIN != "29ABCDE1234F1Z5" {
		t.Fatalf("GSTIN = %q, want it normalised to upper case", view.BuyerGSTIN)
	}
	if view.Status.AppointedDay.String() != "2026-01-25" {
		t.Fatalf("appointed day = %q, want 2026-01-25 (acceptance + 15 days)", view.Status.AppointedDay)
	}
	if view.Status.State != clock.Overdue || view.Status.DaysOverdue != 141 {
		t.Fatalf("status = %q after %d days, want overdue after 141", view.Status.State, view.Status.DaysOverdue)
	}
	if view.Status.Interest <= 0 {
		t.Fatalf("interest = %.2f, want the accrued statutory interest", view.Status.Interest)
	}
	if got.Summary.Overdue != 1 || got.Summary.OutstandingPrincipal != 486500 {
		t.Fatalf("summary = %+v, want one overdue invoice of 486500", got.Summary)
	}
	if len(got.Buyers) != 1 || got.Buyers[0] != "Nandi Agro Equipment Ltd" {
		t.Fatalf("buyers = %v, want the one buyer", got.Buyers)
	}

	// The data file must hold it, and a second store must read it back.
	reopened, err := openStore(path)
	if err != nil {
		t.Fatalf("reopening the data file: %v", err)
	}
	if invoices := reopened.snapshot().Invoices; len(invoices) != 1 || invoices[0].No != "VPW/2026/041" {
		t.Fatalf("the data file holds %+v, want the invoice just added", invoices)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".payment-clock-*.tmp")); len(left) != 0 {
		t.Fatalf("the atomic write left temporary files behind: %v", left)
	}
}

func TestCreateInvoiceRejectsBadInput(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "no invoice number", body: `{"no":"","acceptanceDate":"2026-01-10","amount":1000,"buyerName":"B"}`},
		{name: "no buyer", body: `{"no":"A","acceptanceDate":"2026-01-10","amount":1000,"buyerName":""}`},
		{name: "no amount", body: `{"no":"A","acceptanceDate":"2026-01-10","amount":0,"buyerName":"B"}`},
		{name: "no acceptance date", body: `{"no":"A","acceptanceDate":"","amount":1000,"buyerName":"B"}`},
		{name: "a malformed date", body: `{"no":"A","acceptanceDate":"10-01-2026","amount":1000,"buyerName":"B"}`},
		{name: "a malformed GSTIN", body: `{"no":"A","acceptanceDate":"2026-01-10","amount":1000,"buyerName":"B","buyerGstin":"BAD"}`},
		{name: "a receipt with no date", body: `{"no":"A","acceptanceDate":"2026-01-10","amount":1000,"buyerName":"B","amountReceived":500}`},
		{name: "a receipt over the invoice", body: `{"no":"A","acceptanceDate":"2026-01-10","amount":1000,"buyerName":"B","amountReceived":1500,"receivedDate":"2026-02-01"}`},
		{name: "an unknown field", body: `{"no":"A","acceptanceDate":"2026-01-10","amount":1000,"buyerName":"B","interest":99}`},
		{name: "not JSON at all", body: `not json`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, _, _ := newTestServer(t)
			rec := do(t, h, "POST", "/api/invoices", tt.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("POST = %d, want 400: %s", rec.Code, rec.Body.String())
			}
			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["error"] == "" {
				t.Fatalf("expected an explanation the user can act on, got %s", rec.Body.String())
			}
			state := decodeState(t, do(t, h, "GET", "/api/state", ""))
			if len(state.Invoices) != 0 {
				t.Fatalf("a rejected invoice was stored anyway: %+v", state.Invoices)
			}
		})
	}
}

func TestUpdateAndDeleteInvoice(t *testing.T) {
	h, _, _ := newTestServer(t)
	created := decodeState(t, do(t, h, "POST", "/api/invoices", validInvoice))
	id := created.Invoices[0].ID

	// Recording a payment settles the invoice.
	paid := `{"no":"VPW/2026/041","date":"2026-01-05","acceptanceDate":"2026-01-10",
		"amount":486500,"buyerName":"Nandi Agro Equipment Ltd","buyerGstin":"29ABCDE1234F1Z5",
		"agreedPaymentDate":"","amountReceived":486500,"receivedDate":"2026-03-01"}`
	rec := do(t, h, "PUT", "/api/invoices/"+id, paid)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	got := decodeState(t, rec)
	if len(got.Invoices) != 1 {
		t.Fatalf("got %d invoices after an update, want 1", len(got.Invoices))
	}
	view := got.Invoices[0]
	if view.ID != id {
		t.Fatalf("the record key changed on update: %q then %q", id, view.ID)
	}
	if view.Status.State != clock.Settled {
		t.Fatalf("state = %q after full payment, want settled", view.Status.State)
	}
	if view.Status.DaysOverdue != 35 || view.Status.Interest <= 0 {
		t.Fatalf("a payment 35 days late shows %d days and %.2f interest, want 35 and interest still due",
			view.Status.DaysOverdue, view.Status.Interest)
	}
	if got.Summary.OutstandingPrincipal != 0 {
		t.Fatalf("principal outstanding = %.2f after full payment, want 0", got.Summary.OutstandingPrincipal)
	}

	if rec := do(t, h, "PUT", "/api/invoices/nosuchkey", paid); rec.Code != http.StatusNotFound {
		t.Fatalf("PUT to an unknown key = %d, want 404", rec.Code)
	}
	if rec := do(t, h, "DELETE", "/api/invoices/nosuchkey", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("DELETE of an unknown key = %d, want 404", rec.Code)
	}

	rec = do(t, h, "DELETE", "/api/invoices/"+id, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if left := decodeState(t, rec); len(left.Invoices) != 0 {
		t.Fatalf("got %d invoices after a delete, want none", len(left.Invoices))
	}
}

func TestAsOfDateDrivesTheFigures(t *testing.T) {
	h, _, _ := newTestServer(t)
	do(t, h, "POST", "/api/invoices", validInvoice)

	tests := []struct {
		name        string
		asOf        string
		wantState   clock.State
		wantOverdue int
	}{
		{name: "before the appointed day", asOf: "2026-01-20", wantState: clock.Within},
		{name: "on the appointed day", asOf: "2026-01-25", wantState: clock.Within},
		{name: "a month later", asOf: "2026-02-25", wantState: clock.Overdue, wantOverdue: 31},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decodeState(t, do(t, h, "GET", "/api/state?asOf="+tt.asOf, ""))
			view := got.Invoices[0]
			if view.Status.State != tt.wantState || view.Status.DaysOverdue != tt.wantOverdue {
				t.Fatalf("as on %s: state %q after %d days, want %q after %d",
					tt.asOf, view.Status.State, view.Status.DaysOverdue, tt.wantState, tt.wantOverdue)
			}
			if got.AsOf.String() != tt.asOf {
				t.Fatalf("as-of echoed as %q, want %q", got.AsOf, tt.asOf)
			}
		})
	}

	if rec := do(t, h, "GET", "/api/state?asOf=15-06-2026", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("a malformed as-of date = %d, want 400", rec.Code)
	}
}

func TestSettings(t *testing.T) {
	h, _, path := newTestServer(t)

	good := `{"supplier":{"name":"Vishwakarma Precision Works","udyamNumber":"udyam-kr-03-0001234",
		"address":"Peenya, Bengaluru","contact":"+91 98450 00000","email":"accounts@vpw.example"},
		"bankRatePct":5.75}`
	rec := do(t, h, "POST", "/api/settings", good)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/settings = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	got := decodeState(t, rec)
	if got.Supplier.UdyamNumber != "UDYAM-KR-03-0001234" {
		t.Fatalf("Udyam number = %q, want it normalised", got.Supplier.UdyamNumber)
	}
	if got.BankRatePct != 5.75 || got.AnnualRatePct != 17.25 {
		t.Fatalf("rate = %.2f giving %.2f, want 5.75 giving 17.25", got.BankRatePct, got.AnnualRatePct)
	}

	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the data file: %v", err)
	}
	if !strings.Contains(string(saved), "UDYAM-KR-03-0001234") {
		t.Fatalf("the data file does not hold the saved details:\n%s", saved)
	}

	bad := []struct {
		name string
		body string
	}{
		{name: "a malformed Udyam number", body: `{"supplier":{"udyamNumber":"UDYAM-3-1234"},"bankRatePct":6.5}`},
		{name: "a zero Bank Rate", body: `{"supplier":{},"bankRatePct":0}`},
		{name: "a negative Bank Rate", body: `{"supplier":{},"bankRatePct":-1}`},
		{name: "an absurd Bank Rate", body: `{"supplier":{},"bankRatePct":120}`},
	}
	for _, tt := range bad {
		t.Run(tt.name, func(t *testing.T) {
			if rec := do(t, h, "POST", "/api/settings", tt.body); rec.Code != http.StatusBadRequest {
				t.Fatalf("POST = %d, want 400: %s", rec.Code, rec.Body.String())
			}
		})
	}

	// The rejected settings must not have disturbed the saved ones.
	after := decodeState(t, do(t, h, "GET", "/api/state", ""))
	if after.BankRatePct != 5.75 {
		t.Fatalf("Bank Rate = %.2f after rejected settings, want the saved 5.75", after.BankRatePct)
	}
}

func TestDocuments(t *testing.T) {
	h, _, _ := newTestServer(t)
	do(t, h, "POST", "/api/settings", `{"supplier":{"name":"Vishwakarma Precision Works",
		"udyamNumber":"UDYAM-KR-03-0001234"},"bankRatePct":6.5}`)
	created := decodeState(t, do(t, h, "POST", "/api/invoices", validInvoice))
	id := created.Invoices[0].ID

	tests := []struct {
		name     string
		body     string
		contains []string
	}{
		{
			name:     "reminder",
			body:     `{"kind":"reminder","invoiceId":"` + id + `","buyer":"","asOf":"` + testDay + `"}`,
			contains: []string{"VPW/2026/041", "request for payment", "Nothing has been sent"},
		},
		{
			name:     "notice",
			body:     `{"kind":"notice","invoiceId":"` + id + `","buyer":"","asOf":"` + testDay + `"}`,
			contains: []string{"Notice of demand", "sections 15 and 16", "Bank Rate"},
		},
		{
			name:     "samadhaan packet",
			body:     `{"kind":"samadhaan","invoiceId":"","buyer":"Nandi Agro Equipment Ltd","asOf":"` + testDay + `"}`,
			contains: []string{"MSME SAMADHAAN", "UDYAM-KR-03-0001234", "AMOUNT CLAIMED", "VPW/2026/041"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(t, h, "POST", "/api/documents", tt.body)
			if rec.Code != http.StatusOK {
				t.Fatalf("POST /api/documents = %d, want 200: %s", rec.Code, rec.Body.String())
			}
			var out documentResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
				t.Fatalf("decoding the document: %v", err)
			}
			if out.Title == "" || out.Text == "" {
				t.Fatalf("document came back empty: %+v", out)
			}
			oneLine := strings.Join(strings.Fields(out.Text), " ")
			for _, want := range tt.contains {
				if !strings.Contains(oneLine, strings.Join(strings.Fields(want), " ")) {
					t.Errorf("document is missing %q:\n%s", want, out.Text)
				}
			}
		})
	}

	bad := []struct {
		name string
		body string
		code int
	}{
		{name: "unknown kind", body: `{"kind":"email","invoiceId":"` + id + `"}`, code: http.StatusBadRequest},
		{name: "unknown invoice", body: `{"kind":"notice","invoiceId":"nosuchkey"}`, code: http.StatusNotFound},
		{name: "no buyer chosen", body: `{"kind":"samadhaan","buyer":""}`, code: http.StatusBadRequest},
		{name: "unknown buyer", body: `{"kind":"samadhaan","buyer":"Nobody At All"}`, code: http.StatusNotFound},
		{name: "a malformed as-of date", body: `{"kind":"notice","invoiceId":"` + id + `","asOf":"June"}`, code: http.StatusBadRequest},
	}
	for _, tt := range bad {
		t.Run(tt.name, func(t *testing.T) {
			if rec := do(t, h, "POST", "/api/documents", tt.body); rec.Code != tt.code {
				t.Fatalf("POST = %d, want %d: %s", rec.Code, tt.code, rec.Body.String())
			}
		})
	}
}

func TestEmbeddedUIIsServed(t *testing.T) {
	h, _, _ := newTestServer(t)

	tests := []struct {
		path     string
		contains string
	}{
		{path: "/", contains: "<h1>MSME 45-day payment clock and Samadhaan complaint builder</h1>"},
		{path: "/assets/app.js", contains: "payment clock"},
		{path: "/assets/style.css", contains: "--tool"},
		{path: "/assets/accent.css", contains: "#6d28d9"},
		{path: "/assets/theme-init.js", contains: "omsme-theme"},
		{path: "/assets/favicon.svg", contains: "<svg"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			rec := do(t, h, "GET", tt.path, "")
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200", tt.path, rec.Code)
			}
			if !strings.Contains(rec.Body.String(), tt.contains) {
				t.Fatalf("GET %s does not contain %q", tt.path, tt.contains)
			}
			if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatalf("GET %s is served without nosniff", tt.path)
			}
		})
	}
}

func TestStoreRejectsADamagedFileRatherThanOverwritingIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "payment-clock-data.json")
	if err := os.WriteFile(path, []byte("{ this is not json"), 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	if _, err := openStore(path); err == nil {
		t.Fatal("openStore accepted a damaged data file; the user's records must not be overwritten")
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "{ this is not json" {
		t.Fatalf("the damaged file was changed: %q, %v", raw, err)
	}
}

func TestStoreKeepsADefaultBankRateForOlderFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "payment-clock-data.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"invoices":[]}`), 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	st, err := openStore(path)
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	if got := st.snapshot().BankRatePct; got != clock.BankRateDefaultPct {
		t.Fatalf("BankRatePct = %.2f for a file without one, want the default %.2f", got, clock.BankRateDefaultPct)
	}
}

func TestStoreUpdateLeavesNothingBehindOnAFailedChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "payment-clock-data.json")
	st, err := openStore(path)
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	if _, err := st.update(func(l *ledger) error {
		l.Invoices = append(l.Invoices, clock.Invoice{ID: "1", No: "A"})
		return errNotFound
	}); err == nil {
		t.Fatal("update should have returned the error from its change")
	}
	if invoices := st.snapshot().Invoices; len(invoices) != 0 {
		t.Fatalf("a failed change was kept: %+v", invoices)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("a failed change wrote the data file")
	}
}

func TestNewIDIsUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		id := newID()
		if id == "" {
			t.Fatal("newID returned an empty key")
		}
		if seen[id] {
			t.Fatalf("newID repeated %q", id)
		}
		seen[id] = true
	}
}

func TestServerDefaultsToToday(t *testing.T) {
	path := filepath.Join(t.TempDir(), "payment-clock-data.json")
	st, err := openStore(path)
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	got := newServer(st).today()
	if want := clock.DayOf(time.Now()); !got.Equal(want) {
		t.Fatalf("today() = %q, want %q", got, want)
	}
}
