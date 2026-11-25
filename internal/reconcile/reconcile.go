// Package reconcile imports the gateway export, diffs it against local state and
// replays pending events. It never mutates charges or the ledger.
package reconcile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/sergiodavidfv/webhook-guard/internal/store"
)

// Reconciliation categories.
const (
	CategoryMissingLocal     = "missing_local"
	CategoryMissingRemote    = "missing_remote"
	CategoryAmountMismatch   = "amount_mismatch"
	CategoryStateMismatch    = "state_mismatch"
	CategoryCurrencyMismatch = "currency_mismatch"
	CategoryOK               = "ok"
)

// Export is the gateway's snapshot file (or stdin payload).
type Export struct {
	BatchID  string         `json:"batch_id"`
	Checksum string         `json:"checksum"`
	Charges  []ExportCharge `json:"charges"`
}

// ExportCharge is one charge as reported by the gateway.
type ExportCharge struct {
	ChargeID    string `json:"charge_id"`
	State       string `json:"state"`
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
}

// Report is the serializable reconciliation artifact.
type Report struct {
	BatchID string         `json:"batch_id"`
	Checked int            `json:"checked"`
	Counts  map[string]int `json:"counts"`
	Diffs   []DiffEntry    `json:"diffs"`
}

// DiffEntry is one divergence found by Diff.
type DiffEntry struct {
	ChargeID string `json:"charge_id"`
	Category string `json:"category"`
	Expected any    `json:"expected,omitempty"`
	Actual   any    `json:"actual,omitempty"`
	Source   string `json:"source"`
}

// HasDiffs reports whether any non-ok category counted a divergence.
func (r Report) HasDiffs() bool {
	for _, category := range []string{
		CategoryMissingLocal,
		CategoryMissingRemote,
		CategoryAmountMismatch,
		CategoryStateMismatch,
		CategoryCurrencyMismatch,
	} {
		if r.Counts[category] > 0 {
			return true
		}
	}
	return false
}

// Import validates and stores an export, returning its row id. An export with
// an empty charges list is valid: Diff then reports every local charge as
// missing_remote.
func Import(ctx context.Context, st *store.Store, r io.Reader) (int64, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return 0, fmt.Errorf("read export: %w", err)
	}

	var rawExport struct {
		BatchID  string          `json:"batch_id"`
		Checksum string          `json:"checksum"`
		Charges  json.RawMessage `json:"charges"`
	}
	if err := json.Unmarshal(raw, &rawExport); err != nil {
		return 0, fmt.Errorf("decode export: %w", err)
	}
	if rawExport.BatchID == "" {
		return 0, errors.New("export: missing batch_id")
	}
	if len(rawExport.Charges) == 0 {
		return 0, errors.New("export: missing charges field")
	}

	var charges []ExportCharge
	if err := json.Unmarshal(rawExport.Charges, &charges); err != nil {
		return 0, fmt.Errorf("decode export charges: %w", err)
	}
	if err := validateCharges(charges); err != nil {
		return 0, err
	}

	// The checksum is the SHA-256 of the exact bytes of the charges field as
	// they appear in the export file, so the gateway's own formatting is
	// preserved instead of imposing a canonical re-encoding.
	want := checksumHex(rawExport.Charges)
	if !strings.EqualFold(rawExport.Checksum, want) {
		return 0, fmt.Errorf("export: checksum mismatch (expected %s)", want)
	}

	export := Export{BatchID: rawExport.BatchID, Charges: charges}
	var id int64
	err = st.RunInTx(ctx, func(txCtx context.Context, tx pgx.Tx) error {
		var insertErr error
		id, insertErr = st.InsertExportTx(txCtx, tx, store.ReconciliationExport{
			BatchID:  export.BatchID,
			Checksum: rawExport.Checksum,
			Payload:  json.RawMessage(raw),
		})
		return insertErr
	})
	if err != nil {
		return 0, fmt.Errorf("store export: %w", err)
	}
	return id, nil
}

// Diff compares the stored export against the local charge snapshot. The
// append-only ledger is an audit trail and is not used as an amount total.
func Diff(ctx context.Context, st *store.Store, exportID int64) (Report, error) {
	exportRow, err := st.ExportByID(ctx, exportID)
	if err != nil {
		return Report{}, err
	}

	var export Export
	if err := json.Unmarshal(exportRow.Payload, &export); err != nil {
		return Report{}, fmt.Errorf("decode stored export: %w", err)
	}

	charges, err := st.ListChargesSnapshot(ctx)
	if err != nil {
		return Report{}, err
	}
	return computeReport(export, charges), nil
}

func computeReport(export Export, local []store.Charge) Report {
	report := Report{
		BatchID: export.BatchID,
		Counts: map[string]int{
			CategoryMissingLocal:     0,
			CategoryMissingRemote:    0,
			CategoryAmountMismatch:   0,
			CategoryStateMismatch:    0,
			CategoryCurrencyMismatch: 0,
			CategoryOK:               0,
		},
		Diffs: make([]DiffEntry, 0),
	}

	localByID := make(map[string]store.Charge, len(local))
	for _, charge := range local {
		localByID[charge.ChargeID] = charge
	}

	seen := make(map[string]bool, len(export.Charges))
	for _, expected := range export.Charges {
		seen[expected.ChargeID] = true

		actual, ok := localByID[expected.ChargeID]
		if !ok {
			report.Counts[CategoryMissingLocal]++
			report.Diffs = append(report.Diffs, DiffEntry{
				ChargeID: expected.ChargeID,
				Category: CategoryMissingLocal,
				Expected: expected,
				Source:   "export",
			})
			continue
		}

		mismatch := false
		if string(actual.State) != expected.State {
			mismatch = true
			report.Counts[CategoryStateMismatch]++
			report.Diffs = append(report.Diffs, DiffEntry{
				ChargeID: expected.ChargeID,
				Category: CategoryStateMismatch,
				Expected: expected.State,
				Actual:   string(actual.State),
				Source:   "charges",
			})
		}

		actualAmount := actual.AmountMinor
		if actualAmount != expected.AmountMinor {
			mismatch = true
			report.Counts[CategoryAmountMismatch]++
			report.Diffs = append(report.Diffs, DiffEntry{
				ChargeID: expected.ChargeID,
				Category: CategoryAmountMismatch,
				Expected: expected.AmountMinor,
				Actual:   actualAmount,
				Source:   "charges",
			})
		}

		if actual.Currency != expected.Currency {
			mismatch = true
			report.Counts[CategoryCurrencyMismatch]++
			report.Diffs = append(report.Diffs, DiffEntry{
				ChargeID: expected.ChargeID,
				Category: CategoryCurrencyMismatch,
				Expected: expected.Currency,
				Actual:   actual.Currency,
				Source:   "charges",
			})
		}

		if !mismatch {
			report.Counts[CategoryOK]++
		}
	}

	for _, charge := range local {
		if seen[charge.ChargeID] {
			continue
		}
		report.Counts[CategoryMissingRemote]++
		report.Diffs = append(report.Diffs, DiffEntry{
			ChargeID: charge.ChargeID,
			Category: CategoryMissingRemote,
			Actual:   charge,
			Source:   "charges",
		})
	}

	report.Checked = len(seen) + report.Counts[CategoryMissingRemote]
	return report
}

// ResolveLateEvents is the consumer of the late_events queue: reconciliation
// is the moment a late event gets its final disposition. Applying late effects
// against the ledger is forbidden (the charge already closed), so every
// unresolved late event is reviewed and resolved here; any divergence it
// implies is captured by the reconciliation report itself.
func ResolveLateEvents(ctx context.Context, st *store.Store) (int, error) {
	events, err := st.ListUnresolved(ctx, 0)
	if err != nil {
		return 0, fmt.Errorf("list unresolved late events: %w", err)
	}

	resolved := 0
	for _, ev := range events {
		err := st.RunInTx(ctx, func(txCtx context.Context, tx pgx.Tx) error {
			return st.ResolveTx(txCtx, tx, ev.EventID)
		})
		if err != nil {
			return resolved, fmt.Errorf("resolve late event %s: %w", ev.EventID, err)
		}
		resolved++
	}
	return resolved, nil
}

func validateCharges(charges []ExportCharge) error {
	seen := make(map[string]bool, len(charges))
	for i, charge := range charges {
		if charge.ChargeID == "" {
			return fmt.Errorf("export: charge %d has no charge_id", i)
		}
		if seen[charge.ChargeID] {
			return fmt.Errorf("export: duplicate charge_id %q", charge.ChargeID)
		}
		seen[charge.ChargeID] = true

		switch store.ChargeState(charge.State) {
		case store.StateCreated, store.StateAuthorized, store.StateCaptured, store.StateFailed, store.StateRefunded:
		default:
			return fmt.Errorf("export: charge %q has unknown state %q", charge.ChargeID, charge.State)
		}
		if charge.AmountMinor < 0 {
			return fmt.Errorf("export: charge %q has negative amount_minor", charge.ChargeID)
		}
		if len(charge.Currency) != 3 {
			return fmt.Errorf("export: charge %q has invalid currency %q", charge.ChargeID, charge.Currency)
		}
	}
	return nil
}

// checksumHex returns the SHA-256 hex digest of the raw JSON bytes of the
// charges value. The digest covers the bytes exactly as the gateway wrote
// them, so no canonical re-encoding can reject a valid export.
func checksumHex(raw json.RawMessage) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
