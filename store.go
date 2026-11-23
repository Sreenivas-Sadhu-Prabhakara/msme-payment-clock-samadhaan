package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sreenivas-sadhu-prabhakara/msme-payment-clock-samadhaan/internal/clock"
)

// fileVersion is stamped into the data file so a later version of the tool can
// recognise what it is reading.
const fileVersion = 1

// ledger is everything the tool keeps: the supplier's own details, the Bank
// Rate in force, and the invoices. It is the entire contents of the data file.
type ledger struct {
	Version     int             `json:"version"`
	Supplier    clock.Supplier  `json:"supplier"`
	BankRatePct float64         `json:"bankRatePct"`
	Invoices    []clock.Invoice `json:"invoices"`
}

// store holds the ledger in memory and writes it to one local JSON file.
// Nothing is sent anywhere: the file is the whole database.
type store struct {
	mu   sync.Mutex
	path string
	data ledger
}

// openStore reads the data file, or starts an empty ledger when there is none
// yet. A file that exists but cannot be parsed is an error rather than
// something to overwrite — the user's records are not ours to discard.
func openStore(path string) (*store, error) {
	s := &store{
		path: path,
		data: ledger{Version: fileVersion, BankRatePct: clock.BankRateDefaultPct},
	}
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return s, nil
	case err != nil:
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var data ledger
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("%s is not a data file this tool wrote: %w", path, err)
	}
	if data.BankRatePct <= 0 {
		data.BankRatePct = clock.BankRateDefaultPct
	}
	data.Version = fileVersion
	s.data = data
	return s, nil
}

// snapshot returns a copy of the ledger that the caller can read freely.
func (s *store) snapshot() ledger {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.copyLocked()
}

// update applies fn to the ledger and writes the file. The ledger is only
// changed in memory if the write succeeds, so a failed write leaves the tool
// and the file saying the same thing.
func (s *store) update(fn func(*ledger) error) (ledger, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := s.copyLocked()
	if err := fn(&next); err != nil {
		return ledger{}, err
	}
	previous := s.data
	s.data = next
	if err := s.writeLocked(); err != nil {
		s.data = previous
		return ledger{}, err
	}
	return s.copyLocked(), nil
}

func (s *store) copyLocked() ledger {
	out := s.data
	out.Invoices = make([]clock.Invoice, len(s.data.Invoices))
	copy(out.Invoices, s.data.Invoices)
	return out
}

// writeLocked writes the ledger to a temporary file in the same directory and
// renames it over the data file, so an interrupted write cannot truncate the
// user's records.
func (s *store) writeLocked() error {
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')

	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, ".payment-clock-*.tmp")
	if err != nil {
		return fmt.Errorf("writing %s: %w", s.path, err)
	}
	name := tmp.Name()
	defer os.Remove(name) // a no-op once the rename has succeeded

	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return fmt.Errorf("writing %s: %w", s.path, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("writing %s: %w", s.path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing %s: %w", s.path, err)
	}
	if err := os.Rename(name, s.path); err != nil {
		return fmt.Errorf("replacing %s: %w", s.path, err)
	}
	return nil
}

// newID returns a short random key for a new invoice record. It identifies the
// row in the local file and in the UI; it is not part of the statutory record.
func newID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand does not fail in practice; fall back to something still
		// unique rather than handing out an empty key.
		return fmt.Sprintf("t%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
