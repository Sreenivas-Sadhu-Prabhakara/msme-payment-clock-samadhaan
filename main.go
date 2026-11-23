// Command msme-payment-clock-samadhaan runs the MSME 45-day payment clock and
// Samadhaan complaint builder on your own machine.
//
// It serves a small web UI on 127.0.0.1 and keeps everything in one local JSON
// file. It makes no outbound connections of any kind, and it never sends,
// emails or files anything: it prepares documents for you to issue yourself.
package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/sreenivas-sadhu-prabhakara/msme-payment-clock-samadhaan/internal/clock"
)

//go:embed web
var webFiles embed.FS

// maxBodyBytes caps a request body. The UI sends a few hundred bytes; anything
// larger is a mistake, and reading it unbounded would be one too.
const maxBodyBytes = 1 << 20

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "address to listen on; keep it on 127.0.0.1 so the tool stays on this machine")
	data := flag.String("data", "./payment-clock-data.json", "path to the local data file")
	flag.Parse()

	st, err := openStore(*data)
	if err != nil {
		log.Fatalf("payment clock: %v", err)
	}

	srv := &http.Server{
		Addr:              *addr,
		Handler:           newServer(st).routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Printf("MSME 45-day payment clock")
	log.Printf("open http://%s/ in your browser", *addr)
	log.Printf("data file: %s (nothing leaves this machine)", *data)
	log.Printf("press Ctrl+C to stop")

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("payment clock: %v", err)
	}
}

// server wires the JSON API and the embedded UI over one store.
type server struct {
	store *store
	// today is injectable so the handlers can be tested on a fixed date.
	today func() clock.Day
}

func newServer(st *store) *server {
	return &server{
		store: st,
		today: func() clock.Day { return clock.DayOf(time.Now()) },
	}
}

func (s *server) routes() http.Handler {
	ui, err := fs.Sub(webFiles, "web")
	if err != nil {
		// The directory is embedded at build time; this cannot fail at runtime.
		panic(fmt.Sprintf("embedded UI missing: %v", err))
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", s.handleState)
	mux.HandleFunc("POST /api/invoices", s.handleCreateInvoice)
	mux.HandleFunc("PUT /api/invoices/{id}", s.handleUpdateInvoice)
	mux.HandleFunc("DELETE /api/invoices/{id}", s.handleDeleteInvoice)
	mux.HandleFunc("POST /api/settings", s.handleSettings)
	mux.HandleFunc("POST /api/documents", s.handleDocument)
	mux.Handle("GET /", http.FileServerFS(ui))
	return headers(mux)
}

// headers adds the few response headers a local tool should still set.
func headers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

// stateResponse is what the UI renders: the stored ledger plus everything the
// domain computes from it on the chosen date.
type stateResponse struct {
	Supplier           clock.Supplier `json:"supplier"`
	BankRatePct        float64        `json:"bankRatePct"`
	BankRateDefaultPct float64        `json:"bankRateDefaultPct"`
	AnnualRatePct      float64        `json:"annualRatePct"`
	InterestMultiple   int            `json:"interestMultiple"`
	NoAgreementDays    int            `json:"noAgreementDays"`
	MaxAgreedDays      int            `json:"maxAgreedDays"`
	AsOf               clock.Day      `json:"asOf"`
	Today              clock.Day      `json:"today"`
	Invoices           []invoiceView  `json:"invoices"`
	Buyers             []string       `json:"buyers"`
	Summary            clock.Summary  `json:"summary"`
}

// invoiceView is one stored invoice with its computed position.
type invoiceView struct {
	clock.Invoice
	Status clock.Status `json:"status"`
}

func (s *server) state(asOf clock.Day) stateResponse {
	data := s.store.snapshot()
	views := make([]invoiceView, 0, len(data.Invoices))
	for _, inv := range data.Invoices {
		views = append(views, invoiceView{Invoice: inv, Status: inv.Status(asOf, data.BankRatePct)})
	}
	return stateResponse{
		Supplier:           data.Supplier,
		BankRatePct:        data.BankRatePct,
		BankRateDefaultPct: clock.BankRateDefaultPct,
		AnnualRatePct:      clock.AnnualRatePct(data.BankRatePct),
		InterestMultiple:   clock.InterestMultiple,
		NoAgreementDays:    clock.NoAgreementDays,
		MaxAgreedDays:      clock.MaxAgreedDays,
		AsOf:               asOf,
		Today:              s.today(),
		Invoices:           views,
		Buyers:             clock.Buyers(data.Invoices),
		Summary:            clock.Totals(data.Invoices, asOf, data.BankRatePct),
	}
}

func (s *server) handleState(w http.ResponseWriter, r *http.Request) {
	asOf, err := s.asOf(r.URL.Query().Get("asOf"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, s.state(asOf))
}

func (s *server) handleCreateInvoice(w http.ResponseWriter, r *http.Request) {
	var in clock.Invoice
	if err := readJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	in = tidy(in)
	if err := in.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	in.ID = newID()

	if _, err := s.store.update(func(l *ledger) error {
		l.Invoices = append(l.Invoices, in)
		return nil
	}); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.writeState(w, r, http.StatusCreated)
}

func (s *server) handleUpdateInvoice(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var in clock.Invoice
	if err := readJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	in = tidy(in)
	if err := in.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	in.ID = id

	_, err := s.store.update(func(l *ledger) error {
		for i := range l.Invoices {
			if l.Invoices[i].ID == id {
				l.Invoices[i] = in
				return nil
			}
		}
		return errNotFound
	})
	switch {
	case errors.Is(err, errNotFound):
		writeError(w, http.StatusNotFound, fmt.Errorf("no invoice with that record key"))
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.writeState(w, r, http.StatusOK)
}

func (s *server) handleDeleteInvoice(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, err := s.store.update(func(l *ledger) error {
		for i := range l.Invoices {
			if l.Invoices[i].ID == id {
				l.Invoices = append(l.Invoices[:i], l.Invoices[i+1:]...)
				return nil
			}
		}
		return errNotFound
	})
	switch {
	case errors.Is(err, errNotFound):
		writeError(w, http.StatusNotFound, fmt.Errorf("no invoice with that record key"))
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.writeState(w, r, http.StatusOK)
}

// settingsRequest carries the supplier's own details and the Bank Rate to use.
type settingsRequest struct {
	Supplier    clock.Supplier `json:"supplier"`
	BankRatePct float64        `json:"bankRatePct"`
}

// maxBankRatePct is a sanity bound on the overridable Bank Rate, not a
// statutory figure: three times anything above this would not be a Bank Rate.
const maxBankRatePct = 50

func (s *server) handleSettings(w http.ResponseWriter, r *http.Request) {
	var in settingsRequest
	if err := readJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	in.Supplier.Name = strings.TrimSpace(in.Supplier.Name)
	in.Supplier.UdyamNumber = clock.NormaliseUdyam(in.Supplier.UdyamNumber)
	in.Supplier.Address = strings.TrimSpace(in.Supplier.Address)
	in.Supplier.Contact = strings.TrimSpace(in.Supplier.Contact)
	in.Supplier.Email = strings.TrimSpace(in.Supplier.Email)

	if err := clock.ValidateUdyam(in.Supplier.UdyamNumber); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if in.BankRatePct <= 0 || in.BankRatePct > maxBankRatePct {
		writeError(w, http.StatusBadRequest, fmt.Errorf("enter the Reserve Bank of India Bank Rate as a percentage between 0 and %d", maxBankRatePct))
		return
	}

	if _, err := s.store.update(func(l *ledger) error {
		l.Supplier = in.Supplier
		l.BankRatePct = in.BankRatePct
		return nil
	}); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.writeState(w, r, http.StatusOK)
}

// documentRequest asks for one printable document. Nothing is sent or filed:
// the text comes back for the user to print and issue themselves.
type documentRequest struct {
	Kind      string `json:"kind"`
	InvoiceID string `json:"invoiceId"`
	Buyer     string `json:"buyer"`
	AsOf      string `json:"asOf"`
}

type documentResponse struct {
	Kind  string `json:"kind"`
	Title string `json:"title"`
	Text  string `json:"text"`
	AsOf  string `json:"asOf"`
}

func (s *server) handleDocument(w http.ResponseWriter, r *http.Request) {
	var in documentRequest
	if err := readJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	asOf, err := s.asOf(in.AsOf)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	data := s.store.snapshot()
	out := documentResponse{Kind: in.Kind, AsOf: asOf.String()}

	switch in.Kind {
	case "reminder", "notice":
		inv, ok := find(data.Invoices, in.InvoiceID)
		if !ok {
			writeError(w, http.StatusNotFound, fmt.Errorf("no invoice with that record key"))
			return
		}
		if in.Kind == "reminder" {
			out.Title = "Payment reminder — invoice " + inv.No
			out.Text = clock.ReminderLetter(data.Supplier, inv, asOf, data.BankRatePct)
		} else {
			out.Title = "Notice of demand — invoice " + inv.No
			out.Text = clock.FormalNotice(data.Supplier, inv, asOf, data.BankRatePct)
		}
	case "samadhaan":
		buyer := strings.TrimSpace(in.Buyer)
		if buyer == "" {
			writeError(w, http.StatusBadRequest, fmt.Errorf("choose the buyer the reference is against"))
			return
		}
		if len(clock.ForBuyer(data.Invoices, buyer)) == 0 {
			writeError(w, http.StatusNotFound, fmt.Errorf("no invoices on record for %s", buyer))
			return
		}
		out.Title = "Samadhaan working paper — " + buyer
		out.Text = clock.SamadhaanPacket(data.Supplier, buyer, data.Invoices, asOf, data.BankRatePct)
	default:
		writeError(w, http.StatusBadRequest, fmt.Errorf("unknown document kind %q", in.Kind))
		return
	}
	writeJSON(w, http.StatusOK, out)
}

var errNotFound = errors.New("not found")

// asOf reads the date the figures are computed for, defaulting to today.
func (s *server) asOf(raw string) (clock.Day, error) {
	day, err := clock.ParseDay(raw)
	if err != nil {
		return clock.Day{}, err
	}
	if day.IsZero() {
		return s.today(), nil
	}
	return day, nil
}

func (s *server) writeState(w http.ResponseWriter, r *http.Request, code int) {
	asOf, err := s.asOf(r.URL.Query().Get("asOf"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, code, s.state(asOf))
}

func find(invoices []clock.Invoice, id string) (clock.Invoice, bool) {
	for _, inv := range invoices {
		if inv.ID == id {
			return inv, true
		}
	}
	return clock.Invoice{}, false
}

// tidy trims and normalises what came in from the form before it is validated
// or stored.
func tidy(in clock.Invoice) clock.Invoice {
	in.No = strings.TrimSpace(in.No)
	in.BuyerName = strings.TrimSpace(in.BuyerName)
	in.BuyerGSTIN = clock.NormaliseGSTIN(in.BuyerGSTIN)
	in.Amount = clock.Round2(in.Amount)
	in.AmountReceived = clock.Round2(in.AmountReceived)
	return in
}

func readJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("could not read the form: %w", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("payment clock: writing response: %v", err)
	}
}

func writeError(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}
